"""Read-only, explicitly configured GeminiProxy usage command."""


def register(ctx):
    def command(raw_args):
        if raw_args.strip() not in ('', '--json'):
            return 'Usage: /agy-usage [--json] (read-only proxy-wide statistics)'
        base_url = ctx.get_config('base_url')
        if not base_url:
            return 'agy-usage: configure plugins.entries.agy-usage.settings.base_url explicitly.'
        from .handler import UsageError, fetch_usage, render_text
        import json
        try:
            data = fetch_usage(base_url)
        except UsageError as exc:
            return 'agy-usage: ' + str(exc)
        return json.dumps(data, indent=2, sort_keys=True) if raw_args.strip() == '--json' else render_text(data, base_url)

    ctx.register_command('agy-usage', command, description='Show proxy-wide observed usage', args_hint='[--json]')
