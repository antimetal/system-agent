#!/usr/bin/env python3

import argparse
import os
from pathlib import Path

parser = argparse.ArgumentParser(description="Check that license header is included in Go files.")
parser.add_argument(
    "-v", "--verbose",
    help="Print verbose output.",
    action="store_true",
)
parser.add_argument(
  "-w", "--write",
  help="If file does not start with the header, it will be written to the file in place.",
  action="store_true",
)

args = parser.parse_args()

with open(Path(__file__).with_name("license_header.txt")) as f:
    header = f.read().strip()

exclude_dirs = (
  './.git',
  './vendor',
  './pkg/api',
)

def check_file(file_path: str) -> None:
    for dir in exclude_dirs:
        if file_path.startswith(dir):
            return

    _, ext = os.path.splitext(file_path)
    if ext != '.go':
        return

    if args.verbose:
        print(f'Checking {file_path}')

    with open(file_path, 'r+') as file:
        content = file.read()
        if not content.startswith(header.lstrip()):
            if not args.write:
                raise ValueError(f"File {file_path} does not start with the required license header.")
            file.seek(0, os.SEEK_SET)
            file.write(header + '\n' + content)

def iterate_over_files() -> None:
    error = False
    for root, _, files in os.walk('.'):
        for file in files:
            file_path = os.path.join(root, file)
            try:
                check_file(file_path)
            except ValueError as e:
                print(e)
                error = True
    if error:
        raise SystemExit("Some files do not have the required license header.")


if __name__ == "__main__":
  iterate_over_files()
