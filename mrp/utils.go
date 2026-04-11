package mrp

import (
	"hash/fnv"
	maps0 "maps"
	"os"
	"slices"
)

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func Ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// envOrDefault returns the value of the environment variable specified by envKey,
// or fallback if the variable is not set or empty.
func envOrDefault(envKey, fallback string) string {
	value := os.Getenv(envKey)
	if value == "" {
		return fallback
	}
	return value
}

// Map transforms each element of a slice using the given function.
func Map[T any, U any](s []T, fn func(T) U) []U {
	result := make([]U, len(s))
	for i, v := range s {
		result[i] = fn(v)
	}
	return result
}

// Filter returns elements of a slice that satisfy the predicate.
func Filter[T any](s []T, fn func(T) bool) []T {
	result := make([]T, 0, len(s))
	for _, v := range s {
		if fn(v) {
			result = append(result, v)
		}
	}
	return result
}

// Reduce folds a slice into a single value starting from an initial accumulator.
func Reduce[T any, U any](s []T, initial U, fn func(U, T) U) U {
	acc := initial
	for _, v := range s {
		acc = fn(acc, v)
	}
	return acc
}

// Find returns the first element matching the predicate and true, or the zero value and false.
func Find[T any](s []T, fn func(T) bool) (T, bool) {
	for _, v := range s {
		if fn(v) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// FindIndex returns the index of the first element matching the predicate, or -1.
func FindIndex[T any](s []T, fn func(T) bool) int {
	for i, v := range s {
		if fn(v) {
			return i
		}
	}
	return -1
}

// Some returns true if any element satisfies the predicate.
func Some[T any](s []T, fn func(T) bool) bool {
	return slices.ContainsFunc(s, fn)
}

// Every returns true if all elements satisfy the predicate.
func Every[T any](s []T, fn func(T) bool) bool {
	for _, v := range s {
		if !fn(v) {
			return false
		}
	}
	return true
}

// Contains checks if a slice contains the given value.
func Contains[T comparable](s []T, val T) bool {
	return slices.Contains(s, val)
}

// GroupBy groups slice elements by a key derived from each element.
func GroupBy[T any, K comparable](s []T, keyFn func(T) K) map[K][]T {
	result := make(map[K][]T)
	for _, v := range s {
		k := keyFn(v)
		result[k] = append(result[k], v)
	}
	return result
}

// Partition splits a slice into two: elements that match the predicate and those that don't.
func Partition[T any](s []T, fn func(T) bool) (matched []T, unmatched []T) {
	matched = make([]T, 0, len(s))
	unmatched = make([]T, 0, len(s))
	for _, v := range s {
		if fn(v) {
			matched = append(matched, v)
		} else {
			unmatched = append(unmatched, v)
		}
	}
	return
}

// Flatten merges a slice of slices into a single slice.
func Flatten[T any](s [][]T) []T {
	total := 0
	for _, inner := range s {
		total += len(inner)
	}
	result := make([]T, 0, total)
	for _, inner := range s {
		result = append(result, inner...)
	}
	return result
}

// FlatMap maps each element to a slice and flattens the results.
func FlatMap[T any, U any](s []T, fn func(T) []U) []U {
	result := make([]U, 0)
	for _, v := range s {
		result = append(result, fn(v)...)
	}
	return result
}

// Uniq returns unique elements from the slice preserving order.
func Uniq[T comparable](s []T) []T {
	seen := make(map[T]struct{}, len(s))
	result := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// UniqBy returns unique elements using a key function to determine uniqueness.
func UniqBy[T any, K comparable](s []T, keyFn func(T) K) []T {
	seen := make(map[K]struct{}, len(s))
	result := make([]T, 0, len(s))
	for _, v := range s {
		k := keyFn(v)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// Chunk splits a slice into chunks of the given size.
func Chunk[T any](s []T, size int) [][]T {
	if size <= 0 {
		return nil
	}
	chunks := make([][]T, 0, (len(s)+size-1)/size)
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// CountBy counts how many elements satisfy the predicate.
func CountBy[T any](s []T, fn func(T) bool) int {
	count := 0
	for _, v := range s {
		if fn(v) {
			count++
		}
	}
	return count
}

// KeyBy indexes a slice by a key derived from each element. Latter values overwrite earlier ones for duplicate keys.
func KeyBy[T any, K comparable](s []T, keyFn func(T) K) map[K]T {
	result := make(map[K]T, len(s))
	for _, v := range s {
		result[keyFn(v)] = v
	}
	return result
}

// ForEach calls fn for each element with its index.
func ForEach[T any](s []T, fn func(int, T)) {
	for i, v := range s {
		fn(i, v)
	}
}

// Compact removes zero-value elements from a slice.
func Compact[T comparable](s []T) []T {
	var zero T
	result := make([]T, 0, len(s))
	for _, v := range s {
		if v != zero {
			result = append(result, v)
		}
	}
	return result
}

// Keys returns all keys of a map.
func Keys[K comparable, V any](m map[K]V) []K {
	result := make([]K, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// Values returns all values of a map.
func Values[K comparable, V any](m map[K]V) []V {
	result := make([]V, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}

// MapValues transforms each value in a map using the given function.
func MapValues[K comparable, V any, U any](m map[K]V, fn func(V) U) map[K]U {
	result := make(map[K]U, len(m))
	for k, v := range m {
		result[k] = fn(v)
	}
	return result
}

// Pick returns a new map with only the specified keys.
func Pick[K comparable, V any](m map[K]V, keys []K) map[K]V {
	result := make(map[K]V, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			result[k] = v
		}
	}
	return result
}

// Omit returns a new map without the specified keys.
func Omit[K comparable, V any](m map[K]V, keys []K) map[K]V {
	exclude := make(map[K]struct{}, len(keys))
	for _, k := range keys {
		exclude[k] = struct{}{}
	}
	result := make(map[K]V, len(m))
	for k, v := range m {
		if _, skip := exclude[k]; !skip {
			result[k] = v
		}
	}
	return result
}

// Merge combines multiple maps. Later maps overwrite earlier ones for duplicate keys.
func Merge[K comparable, V any](maps ...map[K]V) map[K]V {
	result := make(map[K]V)
	for _, m := range maps {
		maps0.Copy(result, m)
	}
	return result
}

// Invert swaps keys and values. Requires values to be comparable.
func Invert[K comparable, V comparable](m map[K]V) map[V]K {
	result := make(map[V]K, len(m))
	for k, v := range m {
		result[v] = k
	}
	return result
}

// Entry represents a key-value pair from a map.
type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

// Entries converts a map into a slice of Entry pairs.
func Entries[K comparable, V any](m map[K]V) []Entry[K, V] {
	result := make([]Entry[K, V], 0, len(m))
	for k, v := range m {
		result = append(result, Entry[K, V]{Key: k, Value: v})
	}
	return result
}

// FromEntries converts a slice of Entry pairs into a map.
func FromEntries[K comparable, V any](entries []Entry[K, V]) map[K]V {
	result := make(map[K]V, len(entries))
	for _, e := range entries {
		result[e.Key] = e.Value
	}
	return result
}

// FilterMap returns a new map with only entries that satisfy the predicate.
func FilterMap[K comparable, V any](m map[K]V, fn func(K, V) bool) map[K]V {
	result := make(map[K]V)
	for k, v := range m {
		if fn(k, v) {
			result[k] = v
		}
	}
	return result
}
