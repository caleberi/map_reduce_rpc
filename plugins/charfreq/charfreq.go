// Plugin: charfreq
//
// Builds a character-frequency distribution across all input documents.
// Only printable, non-whitespace Unicode characters are counted.
// The output maps each character (as a string) to its total occurrence count.
//
// This is useful for language detection, encoding analysis, and
// cryptographic frequency analysis.
//
// Build:
//
//	go build -buildmode=plugin \
//	  -o plugins/charfreq/charfreq.so \
//	  ./plugins/charfreq/charfreq.go

package main

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

// Map emits (char, "1") for every printable, non-space rune.
func Map(_ string, contents string) []mrp.KeyValue {
	runes := []rune(strings.ToLower(contents))
	kvs := make([]mrp.KeyValue, 0, len(runes))
	for _, r := range runes {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) {
			continue
		}
		kvs = append(kvs, mrp.KeyValue{Key: string(r), Value: "1"})
	}
	return kvs
}

// Reduce sums occurrence counts for each character.
func Reduce(_ string, values []string) string {
	total := 0
	for _, v := range values {
		n, err := strconv.Atoi(v)
		if err == nil {
			total += n
		}
	}
	return strconv.Itoa(total)
}
