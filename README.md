# ncache [![GoDoc](https://godoc.org/github.com/nussjustin/ncache?status.svg)](https://godoc.org/github.com/nussjustin/ncache) [![Build Status](https://travis-ci.org/nussjustin/ncache.svg?branch=master)](https://travis-ci.org/nussjustin/ncache) [![Go Report Card](https://goreportcard.com/badge/github.com/nussjustin/ncache)](https://goreportcard.com/report/github.com/nussjustin/ncache)

Package ncache implements a simple LRU cache as well as a cache wrapper for populating caches that deduplicates lookups for the same cache key.

## Installation

ncache uses [go modules](https://github.com/golang/go/wiki/Modules) and can be installed via `go get`.

```bash
go get github.com/nussjustin/ncache
```

## Todos

* Examples
* Readthrough cache

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

Please make sure to update tests as appropriate.

## License
[MIT](https://choosealicense.com/licenses/mit/)