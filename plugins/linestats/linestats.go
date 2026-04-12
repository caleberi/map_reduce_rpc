// Plugin: linestats
//
// Computes three aggregate metrics across all processed files:
//
//	"lines" → total number of lines
//	"words" → total number of whitespace-separated tokens
//	"chars" → total number of Unicode characters (runes)
//
// This mirrors the output of `wc -lwm` but distributed across the cluster.
//
// Build:
//
//	go build -buildmode=plugin \
//	  -o plugins/linestats/linestats.so \
//	  ./plugins/linestats/linestats.go

package main

import (
	"strconv"
	"strings"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

// Map counts lines, words, and chars in one document chunk.
func Map(_ string, contents string) []mrp.KeyValue {
	lines := strings.Count(contents, "\n")
	// If the file doesn't end with a newline, count the last partial line.
	if len(contents) > 0 && contents[len(contents)-1] != '\n' {
		lines++
	}
	words := len(strings.Fields(contents))
	chars := len([]rune(contents))

	return []mrp.KeyValue{
		{Key: "lines", Value: strconv.Itoa(lines)},
		{Key: "words", Value: strconv.Itoa(words)},
		{Key: "chars", Value: strconv.Itoa(chars)},
	}
}

// Reduce sums counts across all chunks for each metric key.
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
