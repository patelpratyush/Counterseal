#!/usr/bin/env python3
"""Requires dashboard production build and Python Playwright with Chromium."""
import importlib.util
import pathlib

spec=importlib.util.spec_from_file_location('smoke_gateway',pathlib.Path(__file__).with_name('smoke-gateway.py'))
smoke=importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)
smoke.main(['python3','dashboard/tests/browser.py'])
