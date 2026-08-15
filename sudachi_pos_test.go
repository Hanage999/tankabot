package tankabot

import (
	"strings"
	"testing"
)

func TestAuditedSudachiPOSHierarchySnapshot(t *testing.T) {
	const auditedHierarchyCount = 52
	if got := len(supportedSudachiPOSHierarchies); got != auditedHierarchyCount {
		t.Fatalf("supported hierarchy count = %d, want %d", got, auditedHierarchyCount)
	}

	for hierarchy := range supportedSudachiPOSHierarchies {
		pos := []string{hierarchy[0], hierarchy[1], hierarchy[2], hierarchy[3], "*", "*"}
		if err := validateSudachiPartOfSpeech(pos); err != nil {
			t.Errorf("audited hierarchy %v was rejected: %v", hierarchy, err)
		}
	}
}

func TestMeCabOnlyPOSCategoriesAreRejected(t *testing.T) {
	tests := [][]string{
		{"動詞", "非自立", "*", "*", "五段-ラ行", "終止形-一般"},
		{"名詞", "接尾", "一般", "*", "*", "*"},
	}
	for _, pos := range tests {
		err := validateSudachiPartOfSpeech(pos)
		if err == nil || !strings.Contains(err.Error(), "未対応の品詞階層") {
			t.Errorf("validateSudachiPartOfSpeech(%v) error = %v", pos, err)
		}
	}
}
