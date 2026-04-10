package taskfile

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// applyTaskComments parses the YAML AST to extract comments above task keys
// and uses them as task descriptions when no explicit desc is set.
func applyTaskComments(tf *Taskfile, data []byte) {
	file, err := parser.ParseBytes(data, parser.ParseComments)
	if err != nil || len(file.Docs) == 0 {
		return
	}

	mapping, ok := file.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		return
	}

	taskMapping := findTasksMapping(mapping)
	if taskMapping == nil {
		return
	}

	for _, taskMV := range taskMapping.Values {
		taskKey, ok := taskMV.Key.(*ast.StringNode)
		if !ok {
			continue
		}

		desc := extractCommentText(taskMV)
		if desc == "" {
			continue
		}

		if task, exists := tf.Tasks[taskKey.Value]; exists {
			task.Desc = desc
			tf.Tasks[taskKey.Value] = task
		}
	}
}

// findTasksMapping locates the "tasks" mapping node in the top-level YAML document.
func findTasksMapping(mapping *ast.MappingNode) *ast.MappingNode {
	for _, mv := range mapping.Values {
		key, ok := mv.Key.(*ast.StringNode)
		if !ok || key.Value != "tasks" {
			continue
		}
		result, _ := mv.Value.(*ast.MappingNode)
		return result
	}
	return nil
}

// extractCommentText returns the text from comments above a YAML mapping value.
func extractCommentText(node *ast.MappingValueNode) string {
	comment := node.GetComment()
	if comment == nil {
		return ""
	}

	var b strings.Builder
	for _, c := range comment.Comments {
		if text := strings.TrimSpace(strings.TrimPrefix(c.Token.Value, "#")); text != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(text)
		}
	}
	return b.String()
}
