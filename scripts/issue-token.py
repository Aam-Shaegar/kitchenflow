#!/usr/bin/env python3
"""Issue a random credential into an operator-owned local registry; print token once."""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import secrets
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--file', default='secrets/kitchenflow-auth.json')
parser.add_argument('--subject', required=True)
parser.add_argument('--role', choices=['user', 'partner', 'admin'], required=True)
parser.add_argument('--restaurant', action='append', default=[], type=uuid.UUID)
parser.add_argument('--days', type=int, default=30)
args = parser.parse_args()
if not args.subject.strip() or len(args.subject.encode()) > 128 or not 1 <= args.days <= 365:
    parser.error('Subject must be nonblank, at most 128 bytes; days must be 1–365')
if args.role == 'partner' and not args.restaurant:
    parser.error('Partner needs at least one --restaurant UUID')
path = pathlib.Path(args.file)
path.parent.mkdir(parents=True, exist_ok=True)
entries = json.loads(path.read_text()) if path.exists() else []
token = secrets.token_urlsafe(32)
entries.append(dict(subject=args.subject, role=args.role,
                    restaurant_ids=[str(r) for r in args.restaurant],
                    expires_at=(datetime.datetime.now(datetime.timezone.utc) +
                                datetime.timedelta(days=args.days)).isoformat(),
                    token_sha256=hashlib.sha256(token.encode()).hexdigest()))
# Exclusive temporary file; do not follow a preexisting symlink for output.
temporary = path.with_name(path.name + '.' + secrets.token_hex(8))
try:
    with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        json.dump(entries, stream, indent=2)
    os.replace(temporary, path)
finally:
    temporary.unlink(missing_ok=True)
print(token)
