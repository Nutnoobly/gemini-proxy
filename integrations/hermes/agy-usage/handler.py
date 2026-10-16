"""Dependency-free HTTP reader for GeminiProxy's usage endpoint."""
import json
from datetime import datetime
from http.client import HTTPException
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


COUNTERS = (
    ('completed_requests', 'Completed'), ('measured_requests', 'Measured'),
    ('unknown_usage_requests', 'Unknown'), ('prompt_tokens', 'Input'),
    ('completion_tokens', 'Output'), ('total_tokens', 'Total'),
    ('cached_tokens', 'Cached (input subset)'), ('reasoning_tokens', 'Reasoning (output subset)'),
)


def render_text(data, base_url):
    lines = ['Proxy-wide observed usage', 'Configured proxy URL: ' + base_url,
             'Observed upstream completions; not session usage or account quota.',
             'Tracking started: ' + data['tracking_started_at'],
             'Last completion: ' + data['last_completed_at']]
    lines.extend(f'{label}: {data[key]}' for key, label in COUNTERS)
    for model, usage in sorted(data['models'].items()):
        lines.append('Model ' + model + ': ' + '; '.join(f'{label}: {usage[key]}' for key, label in COUNTERS))
    persistence = data['persistence']
    if not persistence['enabled'] or not persistence['healthy']:
        lines.append('WARNING: persistence ' + ('enabled' if persistence['enabled'] else 'disabled')
                     + ', ' + ('healthy' if persistence['healthy'] else 'unhealthy')
                     + '; totals may not survive restart.')
    return '\n'.join(lines)


class UsageError(Exception):
    """Safe, user-facing diagnostic without server payloads or credentials."""


def validate_usage(data):
    try:
        if not isinstance(data, dict) or type(data['schema_version']) is not int or data['schema_version'] != 1:
            raise ValueError
        def counts(value):
            if not isinstance(value, dict):
                raise ValueError
            for key, _ in COUNTERS:
                if type(value[key]) is not int or value[key] < 0:
                    raise ValueError
            if (value['completed_requests'] != value['measured_requests'] + value['unknown_usage_requests']
                    or value['total_tokens'] != value['prompt_tokens'] + value['completion_tokens']
                    or value['cached_tokens'] > value['prompt_tokens']
                    or value['reasoning_tokens'] > value['completion_tokens']):
                raise ValueError
        counts(data)
        if not isinstance(data['models'], dict):
            raise ValueError
        for value in data['models'].values():
            counts(value)
        for key, _ in COUNTERS:
            if sum(value[key] for value in data['models'].values()) != data[key]:
                raise ValueError
        times = []
        for key in ('tracking_started_at', 'last_completed_at'):
            value = data[key]
            if not isinstance(value, str):
                raise ValueError
            parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
            if parsed.tzinfo is None:
                raise ValueError
            times.append(parsed)
        if data['completed_requests'] and times[1] < times[0]:
            raise ValueError
        persistence = data['persistence']
        if (not isinstance(persistence, dict) or type(persistence['enabled']) is not bool
                or type(persistence['healthy']) is not bool
                or not isinstance(persistence.get('error', ''), str)):
            raise ValueError
    except (KeyError, TypeError, ValueError, OverflowError):
        raise UsageError('invalid usage schema (requires schema_version=1 and consistent tracker fields).') from None
    return data


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def usage_endpoint(base_url):
    try:
        if (not isinstance(base_url, str) or not base_url.isascii()
                or any(c.isspace() or ord(c) < 32 or ord(c) == 127 for c in base_url)):
            raise ValueError
        parts = urlsplit(base_url)
        if (parts.scheme not in ('http', 'https') or not parts.hostname
                or parts.username is not None or parts.password is not None
                or parts.query or parts.fragment):
            raise ValueError
        # Accessing port also validates malformed/out-of-range port values.
        _ = parts.port
        prefix = base_url.rstrip('/')
        return prefix + ('/usage' if parts.path.rstrip('/').endswith('/v1') else '/v1/usage')
    except (ValueError, TypeError):
        raise UsageError('invalid base_url; use an explicit ASCII http(s) API base URL with percent-encoded paths and without credentials, query or fragment.') from None


def strict_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON key')
        result[key] = value
    return result


def reject_constant(value):
    raise ValueError('non-finite JSON number')


MAX_BODY_BYTES = 256 * 1024
TIMEOUT_SECONDS = 3


def fetch_usage(base_url):
    endpoint = usage_endpoint(base_url)
    try:
        with build_opener(NoRedirect()).open(Request(endpoint, method='GET'), timeout=TIMEOUT_SECONDS) as response:
            body = response.read(MAX_BODY_BYTES + 1)
            if len(body) > MAX_BODY_BYTES:
                raise UsageError('usage response too large (limit 256 KiB).')
            try:
                data = json.loads(body.decode('utf-8'), object_pairs_hook=strict_object, parse_constant=reject_constant)
            except (ValueError, UnicodeError, RecursionError):
                raise UsageError('invalid JSON usage response; check configured proxy URL/version.') from None
            return validate_usage(data)
    except HTTPError as exc:
        code = exc.code
        exc.close()
        if 300 <= code < 400:
            message = 'redirect refused; configure the final proxy URL explicitly.'
        elif code in (401, 403):
            message = 'authentication failed; this command does not reuse provider credentials.'
        elif code == 404:
            message = 'usage endpoint unavailable; check configured proxy URL and proxy version.'
        else:
            message = f'proxy returned HTTP {code}.'
        raise UsageError(message) from None
    except TimeoutError:
        raise UsageError('proxy request timed out.') from None
    except URLError as exc:
        message = 'proxy request timed out.' if isinstance(exc.reason, TimeoutError) else 'proxy unreachable; check configured URL and service.'
        raise UsageError(message) from None
    except HTTPException:
        raise UsageError('invalid HTTP response from proxy.') from None
    except OSError:
        raise UsageError('proxy unreachable; check configured URL and service.') from None
