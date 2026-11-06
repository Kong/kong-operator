#!/usr/bin/env bash
# Prerequisites for the eventgateway chainsaw suite: the shared Kafka cluster
# of the Event Gateway suites, see ../onpremeventgateway/prereq.sh.
exec bash "$(cd "$(dirname "$0")" && pwd)/../onpremeventgateway/prereq.sh" "$@"
