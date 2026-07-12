package usage

import (
	"errors"
	"fmt"
	"strings"
)

const (
	maxModelFilterLength = 512
	maxModelFilterTerms  = 50
)

// ErrInvalidModelFilter 表示模型筛选输入超过安全查询边界。
var ErrInvalidModelFilter = errors.New("模型筛选条件无效")

// ValidateModelFilter 校验模型筛选原始长度和实际模型词条总数。
func ValidateModelFilter(raw string) error {
	if len(raw) > maxModelFilterLength {
		return fmt.Errorf("%w：长度不能超过 %d 字节", ErrInvalidModelFilter, maxModelFilterLength)
	}

	_, _, termCount := parseModelFilter(raw)
	if termCount > maxModelFilterTerms {
		return fmt.Errorf("%w：词条不能超过 %d 个", ErrInvalidModelFilter, maxModelFilterTerms)
	}
	return nil
}

// ParseModelFilter 解析空格分隔的包含/排除模型词条，并在各自集合内保持顺序去重。
func ParseModelFilter(raw string) (includeModels, excludeModels []string) {
	includeModels, excludeModels, _ = parseModelFilter(raw)
	return includeModels, excludeModels
}

func parseModelFilter(raw string) (includeModels, excludeModels []string, termCount int) {
	includeSeen := make(map[string]struct{})
	excludeSeen := make(map[string]struct{})
	appendUnique := func(items *[]string, seen map[string]struct{}, term string) {
		if _, ok := seen[term]; ok {
			return
		}
		seen[term] = struct{}{}
		*items = append(*items, term)
	}

	excludeNext := false
	for _, term := range strings.Fields(raw) {
		if term == "!" {
			excludeNext = true
			continue
		}

		termCount++
		if strings.HasPrefix(term, "!") {
			term = strings.TrimPrefix(term, "!")
			if term != "" {
				appendUnique(&excludeModels, excludeSeen, term)
			}
			excludeNext = false
			continue
		}

		if excludeNext {
			appendUnique(&excludeModels, excludeSeen, term)
			excludeNext = false
			continue
		}
		appendUnique(&includeModels, includeSeen, term)
	}
	return includeModels, excludeModels, termCount
}
