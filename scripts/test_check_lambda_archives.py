import importlib.util
import json
from pathlib import Path
from struct import pack_into
import tempfile
import unittest
from unittest.mock import patch
from zipfile import ZipFile, ZipInfo, BadZipFile

spec = importlib.util.spec_from_file_location('archive_checker', Path(__file__).with_name('check_lambda_archives.py'))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


def elf(machine=183):
    header = bytearray(20)
    header[:4] = b'\x7fELF'
    pack_into('<H', header, 18, machine)
    return bytes(header)


def archive(path, data=None, mode=0o100755, name='bootstrap'):
    entry = ZipInfo(name)
    entry.create_system = 3
    entry.external_attr = mode << 16
    with ZipFile(path, 'w') as package:
        package.writestr(entry, elf() if data is None else data)


class ArchiveTests(unittest.TestCase):
    def test_manifest_entries_drive_validation_without_hardcoded_names(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'dist').mkdir()
            archive(root / 'dist' / 'new-feature-command.zip')
            with patch.object(checker, 'load_lambda_names', return_value=['new-feature-command']) as loader:
                checker.validate_archives(root)
            loader.assert_called_once_with(root)

    def test_missing_and_invalid_zip_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'lambda.zip'
            with self.assertRaises(FileNotFoundError):
                checker.validate_archive(path)
            path.write_bytes(b'not a zip')
            with self.assertRaises(BadZipFile):
                checker.validate_archive(path)

    def test_requires_arm64_elf_and_executable_bootstrap(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'lambda.zip'
            for data, mode, name in [(b'short', 0o100755, 'bootstrap'), (elf(62), 0o100755, 'bootstrap'), (b'BAD!' + elf()[4:], 0o100755, 'bootstrap'), (elf(), 0o100644, 'bootstrap'), (elf(), 0o100755, 'other')]:
                with self.subTest(data=data, mode=mode, name=name):
                    archive(path, data=data, mode=mode, name=name)
                    with self.assertRaises(ValueError):
                        checker.validate_archive(path)
            archive(path)
            checker.validate_archive(path)

    def test_multiple_entries_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'lambda.zip'
            archive(path)
            with ZipFile(path, 'a') as package:
                package.writestr('extra', b'bad')
            with self.assertRaises(ValueError):
                checker.validate_archive(path)

    def test_loads_names_from_go_metadata_command_without_running_go(self):
        class Result:
            stdout = json.dumps(['feature-from-manifest', 'admin'])
        root = Path('/repo')
        with patch.object(checker.shutil, 'which', return_value='go.exe'), patch.object(checker.subprocess, 'run', return_value=Result()) as run:
            self.assertEqual(checker.load_lambda_names(root), ['feature-from-manifest', 'admin'])
        self.assertEqual(run.call_args.args[0], ['go.exe', '-C', str(root / 'tools' / 'deploy'), 'run', './cmd/archive-manifest'])
        self.assertTrue(run.call_args.kwargs['check'])


if __name__ == '__main__':
    unittest.main()
