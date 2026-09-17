#!/usr/bin/env python3
"""Run via python3 scripts/test-postgres.py python3 scripts/smoke-agents.py."""
import importlib.util
import pathlib

spec = importlib.util.spec_from_file_location('smoke_gateway', pathlib.Path(__file__).with_name('smoke-gateway.py'))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)
smoke.main(['uv', 'run', '--directory', 'integrations/openai-agents', '--frozen',
            'python', '-m', 'unittest', '-v', 'test_workflow'])
