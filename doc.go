// Package ncache implements a simple LRU cache as well as a cache wrapper for populating caches that deduplicates
// lookups for the same cache key.
//
// The cache wrapper with the deduplication was inspired by this blog post:
//
//     https://nickcraver.com/blog/2019/08/06/stack-overflow-how-we-do-app-caching/
//
package ncache
