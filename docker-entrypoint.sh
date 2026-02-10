#!/bin/sh
set -e
sql-migrate up
exec slurpee "$@"
