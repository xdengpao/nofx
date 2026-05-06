package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"

	"nofx/testutil"
)

// ============================================================================
// ldflags 构建辅助函数
// Requirements: 1.2, 7.3
// ============================================================================

// BuildLdflags 生成 `-X main.Version=... -X main.BuildTime=... -X main.GitCommit=...` 格式的 ldflags 字符串。
func BuildLdflags(version, buildTime, gitCommit string) string {
	return fmt.Sprintf("-X main.Version=%s -X main.BuildTime=%s -X main.GitCommit=%s", version, buildTime, gitCommit)
}

// ParseLdflags 从 ldflags 字符串中提取 Version、BuildTime 和 GitCommit 的值。
// 返回空字符串表示对应字段未找到。
func ParseLdflags(ldflags string) (version, buildTime, gitCommit string) {
	fields := strings.Fields(ldflags)
	for i, f := range fields {
		if f == "-X" && i+1 < len(fields) {
			kv := fields[i+1]
			if idx := strings.Index(kv, "="); idx != -1 {
				key := kv[:idx]
				val := kv[idx+1:]
				switch key {
				case "main.Version":
					version = val
				case "main.BuildTime":
					buildTime = val
				case "main.GitCommit":
					gitCommit = val
				}
			}
		}
	}
	return
}

// TestBuildLdflags_RoundTrip 验证 BuildLdflags → ParseLdflags 的基本 round-trip 正确性。
func TestBuildLdflags_RoundTrip(t *testing.T) {
	cases := []struct {
		version, buildTime, gitCommit string
	}{
		{"v1.0.0", "2025-01-01T00:00:00Z", "abc1234"},
		{"dev", "unknown", "unknown"},
		{"v0.1.0-dirty", "2026-03-26T12:00:00Z", "f00ba12"},
	}

	for _, tc := range cases {
		ldflags := BuildLdflags(tc.version, tc.buildTime, tc.gitCommit)
		gotV, gotB, gotG := ParseLdflags(ldflags)
		if gotV != tc.version || gotB != tc.buildTime || gotG != tc.gitCommit {
			t.Errorf("round-trip 失败: input=(%q,%q,%q) got=(%q,%q,%q)",
				tc.version, tc.buildTime, tc.gitCommit, gotV, gotB, gotG)
		}
	}
}

// ============================================================================
// Feature: build-debug-run-pipeline, Property 1: ldflags 版本注入格式正确性
// Validates: Requirements 1.2, 7.3
// ============================================================================

// genVersionString 生成随机版本字符串（如 v1.2.3、v0.1.0-dirty、dev 等，不含空格）。
func genVersionString() gopter.Gen {
	return gen.OneGenOf(
		// 语义化版本号
		gopter.CombineGens(
			gen.IntRange(0, 99),
			gen.IntRange(0, 99),
			gen.IntRange(0, 99),
		).Map(func(vals []interface{}) string {
			return fmt.Sprintf("v%d.%d.%d", vals[0].(int), vals[1].(int), vals[2].(int))
		}),
		// 带 -dirty 后缀的版本号
		gopter.CombineGens(
			gen.IntRange(0, 99),
			gen.IntRange(0, 99),
			gen.IntRange(0, 99),
		).Map(func(vals []interface{}) string {
			return fmt.Sprintf("v%d.%d.%d-dirty", vals[0].(int), vals[1].(int), vals[2].(int))
		}),
		// 简单标签
		gen.OneConstOf("dev", "latest", "nightly"),
	)
}

// genISO8601Timestamp 生成随机 ISO 8601 UTC 时间戳（格式 2006-01-02T15:04:05Z）。
func genISO8601Timestamp() gopter.Gen {
	return gopter.CombineGens(
		gen.IntRange(2020, 2030),
		gen.IntRange(1, 12),
		gen.IntRange(1, 28),
		gen.IntRange(0, 23),
		gen.IntRange(0, 59),
		gen.IntRange(0, 59),
	).Map(func(vals []interface{}) string {
		return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02dZ",
			vals[0].(int), vals[1].(int), vals[2].(int),
			vals[3].(int), vals[4].(int), vals[5].(int))
	})
}

// genShortCommitHash 生成 7 位十六进制短 commit hash。
func genShortCommitHash() gopter.Gen {
	return gen.SliceOfN(7, gen.OneConstOf(
		'0', '1', '2', '3', '4', '5', '6', '7',
		'8', '9', 'a', 'b', 'c', 'd', 'e', 'f',
	)).Map(func(chars []rune) string {
		b := make([]byte, len(chars))
		for i, c := range chars {
			b[i] = byte(c)
		}
		return string(b)
	})
}

// TestProperty1_LdflagsRoundTrip 使用 gopter 属性基测试验证 BuildLdflags → ParseLdflags 的 round-trip 正确性。
// 对于任意有效的版本字符串、ISO 8601 时间戳和短 commit hash，
// 生成的 ldflags 字符串经 ParseLdflags 解析后应还原出完全相同的三个值。
func TestProperty1_LdflagsRoundTrip(t *testing.T) {
	parameters := testutil.DefaultTestParameters()
	properties := gopter.NewProperties(parameters)

	properties.Property("BuildLdflags → ParseLdflags round-trip 保持所有字段不变", prop.ForAll(
		func(version, buildTime, gitCommit string) bool {
			ldflags := BuildLdflags(version, buildTime, gitCommit)
			gotV, gotB, gotG := ParseLdflags(ldflags)
			return gotV == version && gotB == buildTime && gotG == gitCommit
		},
		genVersionString(),
		genISO8601Timestamp(),
		genShortCommitHash(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Feature: build-debug-run-pipeline, Property 3: Makefile 目标完备性
// Validates: Requirements 7.2
// ============================================================================

// TestProperty3_MakefileTargetCompleteness 验证 Makefile 包含所有需求规定的目标。
// 遍历每个必需目标名称，检查 Makefile 中存在 `^<target>:` 模式的规则定义。
func TestProperty3_MakefileTargetCompleteness(t *testing.T) {
	content, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("无法读取 Makefile: %v", err)
	}

	requiredTargets := []string{
		"build",
		"run",
		"test",
		"test-backend",
		"test-frontend",
		"dev",
		"docker-up",
		"docker-down",
		"clean",
	}

	for _, target := range requiredTargets {
		pattern := fmt.Sprintf(`(?m)^%s:`, regexp.QuoteMeta(target))
		matched, err := regexp.Match(pattern, content)
		if err != nil {
			t.Fatalf("正则匹配错误（目标 %q）: %v", target, err)
		}
		if !matched {
			t.Errorf("Makefile 缺少必需目标 %q 的规则定义（期望匹配 ^%s:）", target, target)
		}
	}
}

// ============================================================================
// Task 6.1: 验证 Docker 部署配置
// Validates: Requirements 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7
// ============================================================================

// TestDockerCompose_BackendHealthcheck 验证后端服务包含正确的健康检查配置。
func TestDockerCompose_BackendHealthcheck(t *testing.T) {
	content, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("无法读取 docker-compose.yml: %v", err)
	}
	s := string(content)

	// 后端服务使用 wget 健康检查（非 curl，因 Alpine 运行时无 curl）
	if !strings.Contains(s, `"wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"`) {
		t.Error("后端服务健康检查应使用 wget 命令检查 /health 端点")
	}

	// 健康检查参数
	checks := []struct {
		pattern string
		desc    string
	}{
		{"interval: 30s", "后端健康检查 interval 应为 30s"},
		{"timeout: 10s", "后端健康检查 timeout 应为 10s"},
		{"retries: 3", "后端健康检查 retries 应为 3"},
	}
	for _, c := range checks {
		if !strings.Contains(s, c.pattern) {
			t.Errorf("%s（期望包含 %q）", c.desc, c.pattern)
		}
	}
}

// TestDockerCompose_BackendRestart 验证后端服务包含 restart: unless-stopped 策略。
func TestDockerCompose_BackendRestart(t *testing.T) {
	content, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("无法读取 docker-compose.yml: %v", err)
	}

	if !strings.Contains(string(content), "restart: unless-stopped") {
		t.Error("后端服务应包含 restart: unless-stopped 重启策略")
	}
}

// TestDockerCompose_FrontendHealthcheck 验证前端服务包含健康检查配置。
func TestDockerCompose_FrontendHealthcheck(t *testing.T) {
	content, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("无法读取 docker-compose.yml: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, `"wget", "--no-verbose", "--tries=1", "--spider", "http://127.0.0.1/health"`) {
		t.Error("前端服务健康检查应使用 wget 命令检查 /health 端点")
	}
}

// TestDockerCompose_FrontendDependsOn 验证前端服务包含 depends_on 配置。
func TestDockerCompose_FrontendDependsOn(t *testing.T) {
	content, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("无法读取 docker-compose.yml: %v", err)
	}

	if !strings.Contains(string(content), "depends_on") {
		t.Error("前端服务应包含 depends_on 配置")
	}
}

// TestDockerCompose_PortEnvVars 验证端口映射使用环境变量。
func TestDockerCompose_PortEnvVars(t *testing.T) {
	content, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("无法读取 docker-compose.yml: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "${NOFX_BACKEND_PORT:-8080}") {
		t.Error("后端端口映射应使用 ${NOFX_BACKEND_PORT:-8080} 环境变量")
	}
	if !strings.Contains(s, "${NOFX_FRONTEND_PORT:-3000}") {
		t.Error("前端端口映射应使用 ${NOFX_FRONTEND_PORT:-3000} 环境变量")
	}
}

// TestDockerfileBackend_MultiStageAndHealthcheck 验证后端 Dockerfile 包含多阶段构建和 HEALTHCHECK 指令。
func TestDockerfileBackend_MultiStageAndHealthcheck(t *testing.T) {
	content, err := os.ReadFile("docker/Dockerfile.backend")
	if err != nil {
		t.Fatalf("无法读取 docker/Dockerfile.backend: %v", err)
	}
	s := string(content)

	// 多阶段构建：至少包含多个 FROM 指令
	fromPattern := regexp.MustCompile(`(?mi)^FROM\s+`)
	matches := fromPattern.FindAllString(s, -1)
	if len(matches) < 2 {
		t.Errorf("后端 Dockerfile 应包含多阶段构建（至少 2 个 FROM），实际找到 %d 个", len(matches))
	}

	// HEALTHCHECK 指令
	if !strings.Contains(s, "HEALTHCHECK") {
		t.Error("后端 Dockerfile 应包含 HEALTHCHECK 指令")
	}
}

// TestDockerfileFrontend_Healthcheck 验证前端 Dockerfile 包含 HEALTHCHECK 指令。
func TestDockerfileFrontend_Healthcheck(t *testing.T) {
	content, err := os.ReadFile("docker/Dockerfile.frontend")
	if err != nil {
		t.Fatalf("无法读取 docker/Dockerfile.frontend: %v", err)
	}

	if !strings.Contains(string(content), "HEALTHCHECK") {
		t.Error("前端 Dockerfile 应包含 HEALTHCHECK 指令")
	}
}

// ============================================================================
// Task 7.1: 验证环境配置模板和 .gitignore
// Validates: Requirements 6.1, 6.2, 6.3, 6.4, 3.4
// ============================================================================

// TestConfigJsonExample_ExistsAndValidJSON 验证 config.json.example 文件存在且为有效 JSON。
func TestConfigJsonExample_ExistsAndValidJSON(t *testing.T) {
	content, err := os.ReadFile("config.json.example")
	if err != nil {
		t.Fatalf("config.json.example 文件不存在或无法读取: %v", err)
	}

	// config.json.example 包含 // 注释行，需先移除再验证 JSON 有效性
	lines := strings.Split(string(content), "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			cleaned = append(cleaned, line)
		}
	}

	var js interface{}
	if err := json.Unmarshal([]byte(strings.Join(cleaned, "\n")), &js); err != nil {
		t.Errorf("config.json.example 不是有效的 JSON（移除注释后）: %v", err)
	}
}

// TestEnvExample_ExistsAndContainsRequiredVars 验证 .env.example 文件存在且包含必需的环境变量。
func TestEnvExample_ExistsAndContainsRequiredVars(t *testing.T) {
	content, err := os.ReadFile(".env.example")
	if err != nil {
		t.Fatalf(".env.example 文件不存在或无法读取: %v", err)
	}
	s := string(content)

	requiredVars := []string{
		"NOFX_BACKEND_PORT",
		"NOFX_FRONTEND_PORT",
		"NOFX_TIMEZONE",
	}

	for _, v := range requiredVars {
		if !strings.Contains(s, v) {
			t.Errorf(".env.example 缺少必需的环境变量 %q", v)
		}
	}
}

// TestGitignore_ExcludesSensitiveFiles 验证 .gitignore 包含 config.json 和 .env 条目。
func TestGitignore_ExcludesSensitiveFiles(t *testing.T) {
	content, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("无法读取 .gitignore: %v", err)
	}
	s := string(content)

	// 精确匹配行内容，避免匹配 config.json.example
	configJsonPattern := regexp.MustCompile(`(?m)^config\.json\s*$`)
	if !configJsonPattern.MatchString(s) {
		t.Error(".gitignore 应包含 config.json 条目（排除敏感配置文件）")
	}

	envPattern := regexp.MustCompile(`(?m)^\.env\s*$`)
	if !envPattern.MatchString(s) {
		t.Error(".gitignore 应包含 .env 条目（排除环境变量文件）")
	}
}

// TestNginxConf_ApiProxyAndHealth 验证 nginx.conf 包含 /api/ 代理和 /health 端点配置。
func TestNginxConf_ApiProxyAndHealth(t *testing.T) {
	content, err := os.ReadFile("nginx/nginx.conf")
	if err != nil {
		t.Fatalf("无法读取 nginx/nginx.conf: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "location /api/") {
		t.Error("nginx.conf 应包含 /api/ 代理配置（location /api/）")
	}
	if !strings.Contains(s, "proxy_pass") {
		t.Error("nginx.conf 的 /api/ 配置应包含 proxy_pass 指令")
	}
	if !strings.Contains(s, "location /health") {
		t.Error("nginx.conf 应包含 /health 端点配置（location /health）")
	}
}

// TestViteConfig_PortAndApiProxy 验证 vite.config.ts 包含端口 3000 和 /api 代理配置。
func TestViteConfig_PortAndApiProxy(t *testing.T) {
	content, err := os.ReadFile("web/vite.config.ts")
	if err != nil {
		t.Fatalf("无法读取 web/vite.config.ts: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "port: 3000") {
		t.Error("vite.config.ts 应配置开发服务器端口为 3000")
	}
	if !strings.Contains(s, "'/api'") {
		t.Error("vite.config.ts 应包含 /api 代理配置")
	}
	if !strings.Contains(s, "http://localhost:8080") {
		t.Error("vite.config.ts 的 /api 代理应指向 http://localhost:8080")
	}
}

// TestTasksJson_ContainsAllRequiredTasks 验证 .vscode/tasks.json 包含所有必需任务名称。
// Requirements: 7.1
func TestTasksJson_ContainsAllRequiredTasks(t *testing.T) {
	content, err := os.ReadFile(".vscode/tasks.json")
	if err != nil {
		t.Fatalf("无法读取 .vscode/tasks.json: %v", err)
	}
	s := string(content)

	requiredTasks := []string{
		"Backend: Build",
		"Backend: Run",
		"Backend: Test",
		"Frontend: Install",
		"Frontend: Build",
		"Frontend: Test",
		"Docker: Deploy",
		"All: Test",
	}
	for _, task := range requiredTasks {
		if !strings.Contains(s, task) {
			t.Errorf("tasks.json 应包含任务 %q", task)
		}
	}
}

// TestTasksJson_BackendBuildIsDefault 验证 tasks.json 中 Backend: Build 任务设置了 isDefault: true。
// Requirements: 7.1
func TestTasksJson_BackendBuildIsDefault(t *testing.T) {
	content, err := os.ReadFile(".vscode/tasks.json")
	if err != nil {
		t.Fatalf("无法读取 .vscode/tasks.json: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, `"isDefault": true`) {
		t.Error("tasks.json 中 Backend: Build 任务应设置 isDefault: true")
	}
}

// TestLaunchJson_ContainsLaunchAndAttach 验证 .vscode/launch.json 包含 Launch 和 Attach 两个调试配置。
// Requirements: 2.2, 2.3
func TestLaunchJson_ContainsLaunchAndAttach(t *testing.T) {
	content, err := os.ReadFile(".vscode/launch.json")
	if err != nil {
		t.Fatalf("无法读取 .vscode/launch.json: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, `"request": "launch"`) {
		t.Error("launch.json 应包含 Launch 调试配置（request: launch）")
	}
	if !strings.Contains(s, `"request": "attach"`) {
		t.Error("launch.json 应包含 Attach 调试配置（request: attach）")
	}
	if !strings.Contains(s, "2345") {
		t.Error("launch.json 的 Attach 配置应使用端口 2345")
	}
	if !strings.Contains(s, `"mode": "debug"`) {
		t.Error("launch.json 的 Launch 配置应使用 debug 模式")
	}
	if !strings.Contains(s, `"mode": "remote"`) {
		t.Error("launch.json 的 Attach 配置应使用 remote 模式")
	}
}
