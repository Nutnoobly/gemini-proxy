import importlib.util
from pathlib import Path
import tempfile
import unittest

INSTALLER = Path(__file__).resolve().parents[2] / 'install_agy_usage.py'

class InstallerTests(unittest.TestCase):
    def installer(self):
        spec = importlib.util.spec_from_file_location('install_agy_usage', INSTALLER)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_installs_only_selected_plugin_without_overwriting(self):
        module = self.installer()
        source = INSTALLER.parent / 'agy-usage'
        with tempfile.TemporaryDirectory() as temp:
            destination = Path(temp) / 'plugins' / 'agy-usage'
            module.install(source, destination)
            self.assertEqual((destination / 'plugin.yaml').read_bytes(), (source / 'plugin.yaml').read_bytes())
            self.assertTrue((destination / 'handler.py').is_file())
            self.assertFalse((destination / 'tests').exists())
            self.assertFalse((destination / '__pycache__').exists())
            with self.assertRaises(FileExistsError):
                module.install(source, destination)
            self.assertEqual((destination / 'handler.py').read_bytes(), (source / 'handler.py').read_bytes())

    def test_refuses_existing_symlink(self):
        module = self.installer()
        with tempfile.TemporaryDirectory() as temp:
            destination = Path(temp) / 'agy-usage'
            destination.symlink_to(Path(temp) / 'missing')
            with self.assertRaises(FileExistsError):
                module.install(INSTALLER.parent / 'agy-usage', destination)
