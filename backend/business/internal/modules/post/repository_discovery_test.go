package post

import "testing"

func TestDiscoveryCandidateValidation(t *testing.T) {
	for _, test := range []struct {
		name       string
		candidates discoveryCandidates
		valid      bool
	}{
		{"空页", discoveryCandidates{IDs: []uint64{}}, true},
		{"恰好一页", discoveryCandidates{IDs: discoveryTestIDs(DefaultLimit), Count: DefaultLimit}, true},
		{"有下一页", discoveryCandidates{IDs: discoveryTestIDs(DefaultLimit + 1), Count: DefaultLimit + 1}, true},
		{"缺少列表", discoveryCandidates{}, false},
		{"缺少候选", discoveryCandidates{IDs: []uint64{2}, Count: 2}, false},
		{"重复候选", discoveryCandidates{IDs: []uint64{2, 2}, Count: 2}, false},
		{"无效 ID", discoveryCandidates{IDs: []uint64{0}, Count: 1}, false},
		{"超出上限", discoveryCandidates{IDs: discoveryTestIDs(DefaultLimit + 2), Count: DefaultLimit + 2}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validDiscoveryCandidates(discoveryFirstPage, test.candidates); got != test.valid {
				t.Fatalf("校验结果为 %t，预期为 %t", got, test.valid)
			}
		})
	}
}

func discoveryTestIDs(n int) []uint64 {
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(n - i)
	}
	return ids
}
