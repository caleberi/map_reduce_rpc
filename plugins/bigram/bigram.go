// Plugin: bigram
//
// Computes bigram (consecutive two-word pair) frequencies across the corpus.
// Bigrams are useful for phrase analysis, language modelling, and plagiarism
// detection.
//
// The Map phase emits ("word1 word2", "1") for every adjacent pair of
// lower-case alphabetic tokens.  The Reduce phase sums the counts.
//
// Build:
//
//	go build -buildmode=plugin \
//	  -o plugins/bigram/bigram.so \
//	  ./plugins/bigram/bigram.go

package main

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

// Map emits (bigram, "1") for every consecutive word pair.
func Map(_ string, contents string) []mrp.KeyValue {
	words := tokenize(contents)
	if len(words) < 2 {
		return nil
	}
	kvs := make([]mrp.KeyValue, 0, len(words)-1)
	for i := 0; i < len(words)-1; i++ {
		bg := words[i] + " " + words[i+1]
		kvs = append(kvs, mrp.KeyValue{Key: bg, Value: "1"})
	}
	return kvs
}

// Reduce sums bigram occurrence counts.
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
