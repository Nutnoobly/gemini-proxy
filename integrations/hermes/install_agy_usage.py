"""Install the bundled command plugin into the active Hermes profile, without overwriting."""
from pathlib import Path
import shutil


def install(source, destination):
    destination = Path(destination)
    if destination.exists() or destination.is_symlink():
        raise FileExistsError('Plugin destination already exists; review/update it explicitly: ' + str(destination))
    shutil.copytree(source, destination, ignore=shutil.ignore_patterns('tests', '__pycache__', '*.pyc'))
    return destination


def main():
    from hermes_constants import get_hermes_home
    destination = Path(get_hermes_home()) / 'plugins' / 'agy-usage'
    installed = install(Path(__file__).resolve().parent / 'agy-usage', destination)
    print('Installed command plugin: ' + str(installed))
    print('Next: hermes plugins doctor agy-usage --ci')
    print('Configure plugins.entries.agy-usage.settings.base_url, then enable agy-usage.')


if __name__ == '__main__':
    main()
