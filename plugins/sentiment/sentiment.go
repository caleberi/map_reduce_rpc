// Plugin: sentiment
//
// Classifies each word in the input as positive, negative, or neutral and
// tallies scores per file.  The Map phase emits:
//
//	"sentiment:positive" → "1"   for every positive word found
//	"sentiment:negative" → "1"   for every negative word found
//	"sentiment:neutral"  → "1"   for every other word found
//
// The Reduce phase sums the counts and emits the final score string.
//
// Build:
//
//	go build -buildmode=plugin \
//	  -o plugins/sentiment/sentiment.so \
//	  ./plugins/sentiment/sentiment.go
//
// Run a worker:
//
//	go run . -role=worker -addr=localhost:1244 \
//	  -master-address=localhost:1235 \
//	  -dfs-address=localhost:8089 \
//	  -plugin-path=./plugins/sentiment/sentiment.so

package main

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

// positiveWords is a curated set of common positive-sentiment words.
var positiveWords = map[string]bool{
	"good": true, "great": true, "excellent": true, "wonderful": true,
	"amazing": true, "fantastic": true, "outstanding": true, "superb": true,
	"brilliant": true, "love": true, "loved": true, "happy": true,
	"joy": true, "joyful": true, "beautiful": true, "perfect": true,
	"best": true, "better": true, "awesome": true, "pleasant": true,
	"positive": true, "success": true, "successful": true, "win": true,
	"winner": true, "won": true, "nice": true, "helpful": true, "hope": true,
	"hopeful": true, "thankful": true, "grateful": true, "kind": true,
	"kindness": true, "care": true, "caring": true, "bright": true,
	"celebrate": true, "celebration": true, "enjoy": true, "enjoyed": true,
	"impressive": true, "innovative": true, "creative": true, "proud": true,
	"trust": true, "trusted": true, "safe": true, "secure": true,
	"courage": true, "courageous": true, "strong": true, "strength": true,
}

// negativeWords is a curated set of common negative-sentiment words.
var negativeWords = map[string]bool{
	"bad": true, "terrible": true, "awful": true, "horrible": true,
	"dreadful": true, "poor": true, "worst": true, "hate": true,
	"hated": true, "fail": true, "failed": true, "failure": true,
	"sad": true, "unhappy": true, "miserable": true, "ugly": true,
	"wrong": true, "broken": true, "corrupt": true, "evil": true,
	"pain": true, "painful": true, "hurt": true, "loss": true,
	"lose": true, "lost": true, "crisis": true, "disaster": true,
	"problem": true, "problems": true, "issue": true, "issues": true,
	"trouble": true, "troubles": true, "dangerous": true, "damage": true,
	"damaged": true, "destroy": true, "destroyed": true, "weak": true,
	"weakness": true, "fear": true, "fearful": true, "angry": true,
	"anger": true, "rage": true, "violence": true, "violent": true,
	"attack": true, "attacked": true, "threat": true, "threatened": true,
}

// tokenize splits text into lower-case alphabetic words.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

// Map emits sentiment category counts for each word in the document.
func Map(filename string, contents string) []mrp.KeyValue {
	words := tokenize(contents)
	kvs := make([]mrp.KeyValue, 0, len(words))
	for _, w := range words {
		category := "neutral"
		if positiveWords[w] {
			category = "positive"
		} else if negativeWords[w] {
			category = "negative"
		}
		kvs = append(kvs, mrp.KeyValue{Key: "sentiment:" + category, Value: "1"})
	}
	return kvs
}

// Reduce sums the per-category counts.
func Reduce(key string, values []string) string {
	total := 0
	for _, v := range values {
		n, err := strconv.Atoi(v)
		if err == nil {
			total += n
		}
	}
	return strconv.Itoa(total)
}
