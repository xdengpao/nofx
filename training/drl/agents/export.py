from __future__ import annotations

from pathlib import Path
from typing import Any


def export_policy_to_onnx(model_path: str, output_path: str, observation_dim: int, opset: int = 17) -> str:
    try:
        import torch
        from stable_baselines3 import PPO
    except ImportError as exc:
        raise RuntimeError("torch/stable-baselines3未安装，无法导出ONNX模型") from exc

    model = PPO.load(model_path)
    model.policy.eval()
    policy = _policy_wrapper(model.policy, torch)
    dummy_input = torch.zeros((1, observation_dim), dtype=torch.float32)
    Path(output_path).parent.mkdir(parents=True, exist_ok=True)
    torch.onnx.export(
        policy,
        dummy_input,
        output_path,
        input_names=["observation"],
        output_names=["action"],
        dynamic_axes={"observation": {0: "batch"}, "action": {0: "batch"}},
        opset_version=opset,
        dynamo=False,
    )
    return output_path


def _policy_wrapper(policy: Any, torch_module: Any):
    class Wrapper(torch_module.nn.Module):
        def __init__(self, p: Any):
            super().__init__()
            self.policy = p

        def forward(self, observation):
            features = self.policy.extract_features(observation)
            if isinstance(features, tuple):
                latent_pi, _ = self.policy.mlp_extractor(*features)
            else:
                latent_pi, _ = self.policy.mlp_extractor(features)
            actions = self.policy.action_net(latent_pi)
            return torch_module.clamp(actions, -1.0, 1.0)

    return Wrapper(policy)
