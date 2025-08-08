#!/bin/bash

# This script adapts auto-generated cli-arguments.md to the requirements of
# docs.konghq.com:
#   - adds a title section
#   - adds a section explaining how environment variables are mapped onto flags
#   - turns vale linter off for the generated part

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(dirname "${BASH_SOURCE[0]}")/../.."
CRD_REF_DOC="${SCRIPT_ROOT}/docs/cli-arguments.md"
if [[ $# != 1 ]]; then
	echo "Usage: $0 <output-file>"
	exit 1
fi
POST_PROCESSED_DOC="${1}"

# Add a title and turn the vale linter off
echo '---
title: CLI Arguments
---

Learn about the various settings and configurations of the controller can be tweaked
using CLI flags.

## Environment variables

Each flag defined in the table below can also be configured using
an environment variable. The name of the environment variable is `KONG_OPERATOR_`
string followed by the name of flag in uppercase.

For example, `--enable-gateway-controller` can be configured using the following
environment variable:

```
KONG_OPERATOR_ENABLE_GATEWAY_CONTROLLER=false
```

It is recommended that all the configuration is done through environment variables
and not CLI flags.

<!-- vale off -->
' > "${POST_PROCESSED_DOC}"

# Add the generated doc content
cat "${CRD_REF_DOC}" >> "${POST_PROCESSED_DOC}"

# Turn the linter back on. Add a newline first, otherwise parsing breaks.
echo "" >> "${POST_PROCESSED_DOC}"
echo "<!-- vale on -->" >> "${POST_PROCESSED_DOC}"
