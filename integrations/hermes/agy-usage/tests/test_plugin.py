import importlib.util
import pathlib
import sys
import unittest
import json
import threading
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def stats():
    counts = dict(completed_requests=2, measured_requests=1, unknown_usage_requests=1,
                  prompt_tokens=10, completion_tokens=5, total_tokens=15, cached_tokens=3, reasoning_tokens=2)
    return dict(counts, schema_version=1, tracking_started_at='2026-10-16T01:00:00Z',
                last_completed_at='2026-10-16T02:00:00Z', models={'gemini': counts.copy()},
                persistence={'enabled': True, 'healthy': True})


@contextmanager
def server(body=None, status=200, headers=None):
    requests = []
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append((self.command, self.path, dict(self.headers)))
            self.send_response(status)
            for name, value in (headers or {}).items():
                self.send_header(name, value)
            self.end_headers()
            self.wfile.write(body if body is not None else json.dumps(stats()).encode())
        def log_message(self, *args):
            pass
    httpd = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    try:
        yield 'http://127.0.0.1:' + str(httpd.server_port), requests
    finally:
        httpd.shutdown()
        httpd.server_close()
        thread.join()

ROOT = pathlib.Path(__file__).resolve().parents[1]


def load_plugin():
    spec = importlib.util.spec_from_file_location('agy_usage_plugin', ROOT / '__init__.py', submodule_search_locations=[str(ROOT)])
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class Context:
    def __init__(self):
        self.commands = []
        self.settings = {}
        self.counters = {'prompt_tokens': 91}

    def register_command(self, name, handler, **kwargs):
        self.commands.append((name, handler, kwargs))

    def get_config(self, key, default=None):
        return self.settings.get(key, default)

    def __getattr__(self, name):
        raise AssertionError('Forbidden context access: ' + name)


class PluginTests(unittest.TestCase):
    def test_reads_explicit_url_each_time_and_preserves_prefix_json(self):
        ctx = Context()
        load_plugin().register(ctx)
        command = ctx.commands[0][1]
        with server() as (url, requests):
            for suffix, endpoint in [('', '/v1/usage'), ('/v1/', '/v1/usage'), ('/tenant/v1', '/tenant/v1/usage'), ('/tenant', '/tenant/v1/usage')]:
                ctx.settings['base_url'] = url + suffix
                self.assertEqual(json.loads(command('--json')), stats())
                self.assertEqual(requests[-1][0:2], ('GET', endpoint))
                self.assertNotIn('Authorization', requests[-1][2])
        self.assertEqual(ctx.counters, {'prompt_tokens': 91})

    def test_text_labels_scope_subsets_models_timestamps_and_persistence(self):
        ctx = Context()
        load_plugin().register(ctx)
        data = stats()
        data['persistence'] = {'enabled': False, 'healthy': False, 'error': 'disk unavailable'}
        with server(json.dumps(data).encode()) as (url, requests):
            ctx.settings['base_url'] = url + '/v1'
            text = ctx.commands[0][1]('')
            for value in ['Proxy-wide observed usage', url + '/v1', 'not session usage or account quota',
                          '2026-10-16T01:00:00Z', '2026-10-16T02:00:00Z',
                          'Completed: 2', 'Measured: 1', 'Unknown: 1', 'Input: 10', 'Output: 5',
                          'Total: 15', 'Cached (input subset): 3', 'Reasoning (output subset): 2',
                          'gemini', 'WARNING', 'disabled', 'unhealthy']:
                self.assertIn(value, text)
            for argument in ['--reset', '--json other', '--help', '-h']:
                self.assertIn('/agy-usage [--json]', ctx.commands[0][1](argument))
            self.assertEqual(len(requests), 1)

    def test_rejects_invalid_schema_without_leaking_payload(self):
        import copy
        ctx = Context()
        load_plugin().register(ctx)
        invalid = [None, [], {}, dict(stats(), schema_version=2), dict(stats(), schema_version=True)]
        for path, value in [
            (('models',), {'gemini': None}), (('models',), []),
            (('prompt_tokens',), -1), (('total_tokens',), 16),
            (('cached_tokens',), 11), (('reasoning_tokens',), 6),
            (('completed_requests',), 3), (('measured_requests',), True),
            (('completion_tokens',), 5.0), (('prompt_tokens',), '10'),
            (('tracking_started_at',), 'private-payload'),
            (('tracking_started_at',), '2026-10-17T01:00:00Z'),
            (('last_completed_at',), None), (('persistence', 'healthy'), 1),
            (('persistence', 'enabled'), None), (('persistence', 'error'), []),
            (('models', 'gemini', 'total_tokens'), 0),
            (('models', 'gemini', 'completed_requests'), 0),
        ]:
            data = copy.deepcopy(stats())
            target = data
            for key in path[:-1]:
                target = target[key]
            target[path[-1]] = value
            invalid.append(data)
        missing = stats()
        del missing['prompt_tokens']
        invalid.append(missing)
        for data in invalid:
            with self.subTest(data=data), server(json.dumps(data).encode()) as (url, _):
                ctx.settings['base_url'] = url
                text = ctx.commands[0][1]('--json')
                self.assertTrue(text.startswith('agy-usage: invalid usage schema'), text)
                self.assertNotIn('private-payload', text)

    def test_http_and_network_failures_are_concise_and_do_not_leak(self):
        from unittest.mock import patch
        from urllib.error import URLError
        ctx = Context()
        plugin = load_plugin()
        plugin.register(ctx)
        for status, message in [(401, 'authentication'), (403, 'authentication'), (404, 'usage endpoint unavailable'), (500, 'HTTP 500')]:
            with self.subTest(status=status), server(b'private-secret', status=status) as (url, requests):
                ctx.settings['base_url'] = url
                text = ctx.commands[0][1]('')
                self.assertIn(message, text)
                self.assertNotIn('private-secret', text)
                self.assertEqual(len(requests), 1)
        ctx.settings['base_url'] = 'http://127.0.0.1:1/v1'
        for error, message in [(TimeoutError('private-secret'), 'timed out'), (URLError('private-secret'), 'unreachable')]:
            with self.subTest(error=type(error).__name__), patch('urllib.request.OpenerDirector.open', side_effect=error):
                text = ctx.commands[0][1]('')
                self.assertIn(message, text)
                self.assertNotIn('private-secret', text)

    def test_truncated_chunked_response_is_safe_through_command(self):
        ctx = Context()
        load_plugin().register(ctx)
        with server(b'20\r\nprivate-secret', headers={'Transfer-Encoding': 'chunked'}) as (url, requests):
            ctx.settings['base_url'] = url
            text = ctx.commands[0][1]('--json')
            self.assertEqual(text, 'agy-usage: invalid HTTP response from proxy.')
            self.assertNotIn('private-secret', text)
            self.assertEqual([request[:2] for request in requests], [('GET', '/v1/usage')])
        self.assertEqual(ctx.counters, {'prompt_tokens': 91})

    def test_rejects_raw_unicode_url_path_before_request(self):
        ctx = Context()
        load_plugin().register(ctx)
        with server() as (url, requests):
            ctx.settings['base_url'] = url + '/私密-private-secret/v1'
            text = ctx.commands[0][1]('--json')
            self.assertIn('agy-usage: invalid base_url', text)
            self.assertIn('percent-encoded paths', text)
            self.assertNotIn('private-secret', text)
            self.assertNotIn('私密', text)
            self.assertEqual(requests, [])
        self.assertEqual(ctx.counters, {'prompt_tokens': 91})

    def test_percent_encoded_unicode_url_path_succeeds(self):
        ctx = Context()
        load_plugin().register(ctx)
        with server() as (url, requests):
            ctx.settings['base_url'] = url + '/%E7%A7%81%E5%AF%86/v1'
            self.assertEqual(json.loads(ctx.commands[0][1]('--json')), stats())
            self.assertEqual([request[:2] for request in requests],
                             [('GET', '/%E7%A7%81%E5%AF%86/v1/usage')])
        self.assertEqual(ctx.counters, {'prompt_tokens': 91})

    def test_rejects_unsafe_urls_and_never_follows_redirects(self):
        ctx = Context()
        load_plugin().register(ctx)
        for url in [42, True, 'file:///etc/passwd', 'ftp://localhost', 'http://',
                    'http://user:private-secret@localhost/v1', 'http://localhost/v1?key=private-secret',
                    'http://localhost/v1#private-secret', 'http://localhost:bad/v1', 'http://localhost/\nprivate-secret']:
            with self.subTest(url=url):
                ctx.settings['base_url'] = url
                text = ctx.commands[0][1]('')
                self.assertIn('invalid base_url', text)
                self.assertNotIn('private-secret', text)
        with server() as (destination, received):
            for status in [301, 302, 303, 307, 308]:
                with self.subTest(status=status), server(status=status, headers={'Location': destination + '/private'}) as (origin, requests):
                    ctx.settings['base_url'] = origin
                    self.assertIn('redirect refused', ctx.commands[0][1](''))
                    self.assertEqual(len(requests), 1)
            self.assertEqual(received, [])

    def test_rejects_malformed_or_oversized_response_bodies(self):
        ctx = Context()
        load_plugin().register(ctx)
        for body, message in [(b'not JSON private-secret', 'invalid JSON'), (b'\xff', 'invalid JSON'),
                              (b'[' * 2000, 'invalid JSON'),
                              (b'x' * (256 * 1024 + 1), 'too large'),
                              (b'{"schema_version":1,"schema_version":2}', 'invalid JSON'),
                              (b'{"schema_version":NaN}', 'invalid JSON')]:
            with self.subTest(message=message, length=len(body)), server(body) as (url, _):
                ctx.settings['base_url'] = url
                text = ctx.commands[0][1]('--json')
                self.assertIn(message, text)
                self.assertNotIn('private-secret', text)

    def test_registers_only_agy_usage_and_requires_explicit_url(self):
        self.assertTrue((ROOT / '__init__.py').exists(), 'command plugin must exist')
        ctx = Context()
        load_plugin().register(ctx)
        self.assertEqual([c[0] for c in ctx.commands], ['agy-usage'])
        self.assertIn('base_url', ctx.commands[0][1](''))
        self.assertEqual(ctx.counters, {'prompt_tokens': 91})


if __name__ == '__main__':
    unittest.main()
