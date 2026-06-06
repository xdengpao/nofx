#!/usr/bin/env python3
from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from agents.export import export_policy_to_onnx
from env.features import observation_dim


def main() -> int:
    parser = argparse.ArgumentParser(description="Export DRL PPO policy to ONNX")
    parser.add_argument("--model-path", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--observation-window", type=int, default=60)
    parser.add_argument("--opset", type=int, default=17)
    args = parser.parse_args()
    export_policy_to_onnx(args.model_path, args.output, observation_dim(args.observation_window), args.opset)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
