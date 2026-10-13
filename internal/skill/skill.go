// Package skill 提供 Agent Skill (SKILL.md) 文件的模板渲染与动态同步。
package skill

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"sheep-get/internal/atomicfile"
)

//go:embed template.md
var skillTemplateContent string

var parsedTemplate = template.Must(template.New("skill").Parse(skillTemplateContent))

// TemplateParams 包含渲染 SKILL.md 所需的参数。
type TemplateParams struct {
	Port   int
	APIKey string
}

// Render 根据给定的参数渲染 SKILL.md 内容。
func Render(params TemplateParams) ([]byte, error) {
	var buf bytes.Buffer
	if err := parsedTemplate.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("failed to render skill template: %w", err)
	}
	return buf.Bytes(), nil
}

// TargetFile 返回 Skill 文件的相对路径：skills/sheep-get/SKILL.md。
func TargetFile(baseDir string) string {
	return filepath.Join(baseDir, "skills", "sheep-get", "SKILL.md")
}

// TargetDir 返回 Skill 目录的相对路径：skills/sheep-get。
func TargetDir(baseDir string) string {
	return filepath.Join(baseDir, "skills", "sheep-get")
}

// Sync 将渲染后的 SKILL.md 原子写入 targetDir 下的 skills/sheep-get/SKILL.md。
func Sync(baseDir string, port int, apiKey string) error {
	if baseDir == "" {
		return fmt.Errorf("base directory cannot be empty")
	}

	content, err := Render(TemplateParams{
		Port:   port,
		APIKey: apiKey,
	})
	if err != nil {
		return err
	}

	skillDir := TargetDir(baseDir)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create skill directory %s: %w", skillDir, err)
	}

	skillFile := TargetFile(baseDir)
	if err := atomicfile.Write(skillFile, content, 0644, true); err != nil {
		return fmt.Errorf("failed to write skill file %s: %w", skillFile, err)
	}

	return nil
}
