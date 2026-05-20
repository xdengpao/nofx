package optimize

import "errors"

var (
	ErrInsufficientEvidence      = errors.New("缺陷证据不足")
	ErrIncomparableRuns          = errors.New("baseline和candidate不可比")
	ErrMissingStructureSnapshots = errors.New("缺少结构快照")
	ErrUncommittedPolicyOverride = errors.New("阈值覆盖缺少已提交policy引用")
)
