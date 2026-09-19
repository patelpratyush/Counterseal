#!/bin/sh
set -eu
umask 077
if [ "$1" = server ]; then
  if [ ! -e /data/server.priv ] && [ ! -e /data/server.pub ]; then
    handoffguard keygen --out /data/server
  fi
  if [ ! -s /data/server.priv ] || [ ! -s /data/server.pub ]; then
    echo 'Incomplete signing key pair in /data; restore the saved key pair.' >&2
    exit 1
  fi
fi
exec handoffguard "$@"
