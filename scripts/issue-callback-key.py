#!/usr/bin/env python3
"""Issue a callback HMAC key into a local registry; print only its ID."""
import argparse
import datetime
import base64
import json
import os
import pathlib
import secrets
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--file', default='secrets/callback-keys.json')
parser.add_argument('--restaurant', type=uuid.UUID, required=True)
parser.add_argument('--key-id', required=True)
parser.add_argument('--days', type=int, default=30)
args = parser.parse_args()
if not args.key_id or len(args.key_id) > 128 or not 1 <= args.days <= 365 or args.restaurant.int == 0:
    parser.error('Valid key ID, restaurant UUID and days 1–365 required')
path = pathlib.Path(args.file)
path.parent.mkdir(parents=True, exist_ok=True)
entries = json.loads(path.read_text()) if path.exists() else []
if any(entry['key_id'] == args.key_id for entry in entries):
    parser.error('Key ID already exists; use a new ID for rotation')
entries.append(dict(key_id=args.key_id, restaurant_id=str(args.restaurant),
                    expires_at=(datetime.datetime.now(datetime.timezone.utc) +
                                datetime.timedelta(days=args.days)).isoformat(),
                    secret_base64=base64.b64encode(secrets.token_bytes(32)).decode()))
# Exclusive temporary file; do not follow a preexisting symlink for output.
temporary = path.with_name(path.name + '.' + secrets.token_hex(8))
try:
    with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        json.dump(entries, stream, indent=2)
    os.replace(temporary, path)
finally:
    temporary.unlink(missing_ok=True)
print(args.key_id)
