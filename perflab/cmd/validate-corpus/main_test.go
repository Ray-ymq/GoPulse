package main

import (
	"testing"

	"github.com/Ray-ymq/GoPulse/perflab/internal/input"
)

func corpusFixture() input.Corpus {
	return input.Corpus{Users: []input.User{{ID: 11}, {ID: 22}}, ReadPostIDs: []uint64{1},
		InteractionPostIDs: []uint64{2}, EditablePostIDs: [][]uint64{{3}, {4}},
		DeletePostIDs: [][]uint64{{9, 7}, {8}}}
}

func TestTargetsPreserveOwnersAcrossSortedBatches(t *testing.T) {
	fields, minimum, err := corpusTargets(corpusFixture())
	if err != nil || minimum != 1 || len(fields) != 4 {
		t.Fatalf("fields=%v minimum=%d error=%v", fields, minimum, err)
	}
	deletes := fields[3].targets
	if !matchesOwner(deletes[:2], 7, 11) || !matchesOwner(deletes[:2], 8, 22) ||
		matchesOwner(deletes[:2], 8, 11) || matchesOwner(deletes[:2], 9, 11) {
		t.Fatal("排序或分批校验丢失归属")
	}
}

func TestRejectDuplicateOverlapAndMissingOwnedTargets(t *testing.T) {
	for _, kind := range []string{"duplicate_same_actor", "duplicate_other_actor", "edit_duplicate", "read", "interaction", "edit", "empty", "length"} {
		t.Run(kind, func(t *testing.T) {
			corpus := corpusFixture()
			switch kind {
			case "duplicate_same_actor":
				corpus.DeletePostIDs[0] = []uint64{7, 7}
			case "duplicate_other_actor":
				corpus.DeletePostIDs[1] = []uint64{7}
			case "edit_duplicate":
				corpus.EditablePostIDs[1] = []uint64{3}
			case "read":
				corpus.ReadPostIDs = []uint64{7}
			case "interaction":
				corpus.InteractionPostIDs = []uint64{8}
			case "edit":
				corpus.EditablePostIDs[0] = []uint64{9}
			case "empty":
				corpus.DeletePostIDs[1] = nil
			case "length":
				corpus.DeletePostIDs = corpus.DeletePostIDs[:1]
			}
			if _, _, err := corpusTargets(corpus); err == nil {
				t.Fatal("无效语料未被拒绝")
			}
		})
	}
}

func TestFormalDeleteCorpusUsesBoundedTargetStorage(t *testing.T) {
	corpus := corpusFixture()
	corpus.Users = make([]input.User, 512)
	corpus.DeletePostIDs = make([][]uint64, 512)
	corpus.EditablePostIDs = make([][]uint64, 512)
	for actor := range corpus.Users {
		corpus.Users[actor].ID = uint64(actor + 1)
		corpus.EditablePostIDs[actor] = []uint64{uint64(100 + actor)}
		corpus.DeletePostIDs[actor] = make([]uint64, 4127)
		for index := range corpus.DeletePostIDs[actor] {
			corpus.DeletePostIDs[actor][index] = uint64(10000 + actor*4127 + index)
		}
	}
	fields, minimum, err := corpusTargets(corpus)
	if err != nil || minimum != 4127 || len(fields[3].targets) != 512*4127 {
		t.Fatalf("完整删除语料校验失败：minimum=%d error=%v", minimum, err)
	}
	last := fields[3].targets[len(fields[3].targets)-1]
	if !matchesOwner(fields[3].targets, last.id, 512) {
		t.Fatal("末尾目标的归属错误")
	}
}
