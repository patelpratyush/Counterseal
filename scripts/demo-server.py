#!/usr/bin/env python3
"""Exercise a running server: mint, deny, approve, allow, reject replay, audit."""
import json
import os
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone

base = os.environ.get('HANDOFFGUARD_SERVER_URL', 'http://127.0.0.1:8080').rstrip('/')
token = os.environ['HANDOFFGUARD_API_TOKEN']


def call(path, body=None, expected=200):
    request = urllib.request.Request(base + path, data=json.dumps(body).encode(),
        headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    try:
        response = urllib.request.urlopen(request, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        data = json.load(response)
        if response.status != expected:
            raise RuntimeError(f'{path}: HTTP {response.status}: {data}')
    return data


now = datetime.now(timezone.utc)
root = call('/v1/envelopes', {'envelope': {
    'version': '1', 'issuer': {'agent': 'support'}, 'recipient': {'agent': 'billing'},
    'purpose': 'customer_refund', 'policy_version': 'refund-v1',
    'allowed_actions': ['refunds.create'], 'denied_actions': ['customers.delete'],
    'resources': {'orders': ['48319']}, 'data_classes': ['payment_metadata'],
    'approvals': [{'condition': 'refund.amount > 500', 'required_role': 'refund_manager'}],
    'delegation': {'max_depth': 2, 'current_depth': 0, 'may_expand_authority': False},
    'expires_at': (now + timedelta(hours=1)).isoformat(),
}}, expected=201)
id = root['envelope']['id']
arguments = {'order_id': '48319', 'amount': 825}
action = {'envelope_id': id, 'agent_id': 'billing', 'tool': 'refunds.create',
          'resources': {'orders': ['48319']}, 'data_classes': ['payment_metadata'],
          'arguments': arguments}
call('/v1/evaluate/action', action, expected=403)
print('DENY: manager approval required')
call('/v1/approvals', {'envelope_id': id, 'action': 'refunds.create',
    'resource': 'orders:48319', 'arguments': arguments, 'approved_by': 'manager_193',
    'role': 'refund_manager', 'expires_at': (now + timedelta(minutes=10)).isoformat()}, expected=201)
call('/v1/evaluate/action', action)
print('ALLOW: matching approval consumed')
call('/v1/evaluate/action', action, expected=403)
print('DENY: approval replay rejected')
audit = call('/v1/audit/' + root['run_id'] + '/verify')
assert audit['status'] == 'VALID', audit
print('Audit VALID:', root['run_id'], 'head:', audit['head_hash'])
