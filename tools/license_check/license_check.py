#!/usr/bin/env python3

import argparse
import os

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

# Define the header you want to check for and insert
header = """// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
"""

exclude_dirs = (
  './.git',
  './vendor',
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
