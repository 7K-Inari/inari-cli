#!/usr/bin/env bash
set -euo pipefail
PACKAGE_DIR=$(cd "$(dirname "$0")/.." && pwd)
python3 - "$PACKAGE_DIR" <<'PY'
import json
import sys
from pathlib import Path

import jsonschema
import yaml

package = Path(sys.argv[1])
manifest = yaml.safe_load((package / "package.yaml").read_text())
chart = yaml.safe_load((package / "chart.yaml").read_text())
assert manifest["type"] == "platform-app"
assert chart["repoURL"].startswith("oci://")
assert chart["chart"]
schema = json.loads((package / "values.schema.json").read_text())
jsonschema.validators.validator_for(schema).check_schema(schema)
jsonschema.validate(yaml.safe_load((package / "values-defaults.yaml").read_text()), schema)
print("platform-app manifest and values are valid; no local chart was selected")
PY
