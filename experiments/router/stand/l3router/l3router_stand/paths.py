"""Resolved paths for the l3router stand (no runtime I/O)."""

from pathlib import Path

# experiments/router/stand/l3router
STAND_ROOT = Path(__file__).resolve().parent.parent
# hiddify-core/hiddify-sing-box (single source of truth for client/server builds)
SING_BOX_ROOT = STAND_ROOT.parent.parent.parent.parent / "hiddify-core" / "hiddify-sing-box"
ARTIFACTS_DIR = STAND_ROOT / "artifacts"
SCRIPTS_DIR = STAND_ROOT / "scripts"
CONFIGS_DIR = STAND_ROOT / "configs"
