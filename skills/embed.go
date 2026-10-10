// Package skills supplies the skill files shipped with the binary.
package skills

import "embed"

//go:embed */SKILL.md */references/*.md
var Files embed.FS
