package scan

import (
	"bytes"
	"os"
	"sort"
)

// FindDuplicates groups equal hashes while collapsing hard-linked paths.
func FindDuplicates(files []FileRecord) []DuplicateGroup {
	byHash := make(map[[32]byte][]FileRecord)
	for _, file := range files {
		byHash[file.Hash] = append(byHash[file.Hash], file)
	}

	groups := make([]DuplicateGroup, 0)
	for hash, candidates := range byHash {
		if len(candidates) < 2 {
			continue
		}

		distinct := make([]FileRecord, 0, len(candidates))
		for _, candidate := range candidates {
			linked := false
			for _, existing := range distinct {
				if candidate.Info != nil && existing.Info != nil && os.SameFile(candidate.Info, existing.Info) {
					linked = true
					break
				}
			}
			if !linked {
				distinct = append(distinct, candidate)
			}
		}
		if len(distinct) < 2 {
			continue
		}

		sort.Slice(distinct, func(i, j int) bool { return distinct[i].Path < distinct[j].Path })
		size := distinct[0].Size
		groups = append(groups, DuplicateGroup{
			Hash:             hash,
			Size:             size,
			Files:            distinct,
			ReclaimableBytes: size * int64(len(distinct)-1),
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].ReclaimableBytes != groups[j].ReclaimableBytes {
			return groups[i].ReclaimableBytes > groups[j].ReclaimableBytes
		}
		if groups[i].Size != groups[j].Size {
			return groups[i].Size > groups[j].Size
		}
		return bytes.Compare(groups[i].Hash[:], groups[j].Hash[:]) < 0
	})
	return groups
}
