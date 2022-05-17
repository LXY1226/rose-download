module downloader

go 1.18

require (
	github.com/nwaples/rardecode v1.1.3
	golang.org/x/sys v0.0.0-20220503163025-988cb79eb6c6
)

replace github.com/nwaples/rardecode v1.1.3 => ./unrar
