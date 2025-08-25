#!/bin/bash

# This script adapts auto-generated cli-arguments.md to the requirements of
# developer.konghq.com:
#   - adds the front matter
#   - adds a section explaining how environment variables are mapped onto flags
#   - turns vale linter off for the generated part

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(dirname "${BASH_SOURCE[0]}")/../.."
POST_PROCESSED_DOC="${1}"
SOURCE_DOC="${2:-${SCRIPT_ROOT}/docs/cli-arguments.md}"

# Add a title and turn the vale linter off
echo '---
title: "{{site.operator_product_name}} configuration options"
short_title: Configuration options
description: Learn about the various settings and configurations of the operator which can be tweaked using CLI flags.
content_type: reference
layout: reference

breadcrumbs:
  - /operator/
  - index: operator
    group: Reference

products:
  - operator

works_on:
  - on-prem
  - konnect
---

Configuration options allow you to customize the behavior of {{ site.operator_product_name }} to meet your needs.

The default configuration will work for most users. These options are provided for advanced users.

## Using environment variables

Each flag defined in the following table can also be configured using an environment variable.
The name of the environment variable is `KONG_OPERATOR_` string followed by the name of flag in uppercase.

For example, `--secret-label-selector` can be configured using the following environment variable:

```
KONG_OPERATOR_SECRET_LABEL_SELECTOR=mylabel
```

We recommend configuring all settings through environment variables and not CLI flags.

<!-- vale off -->
' > "${POST_PROCESSED_DOC}"

# Add the generated doc content
cat "${SOURCE_DOC}" >> "${POST_PROCESSED_DOC}"

# Turn the linter back on. Add a newline first, otherwise parsing breaks.
echo "" >> "${POST_PROCESSED_DOC}"
echo "<!-- vale on -->" >> "${POST_PROCESSED_DOC}"
