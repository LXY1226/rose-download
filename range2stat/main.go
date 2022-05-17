package main

import "fmt"

func main() {
	ranges := []rg{
		{0x0, 0x61c30000},
		{0x62b90000, 0x2070000},
		{0x65750000, 0x2070000},
		{0x684d0000, 0x1b20000},
		{0x6b080000, 0x6b080000},
	}
	for i := 1; i < len(ranges); i++ {
		end := ranges[i-1].len + ranges[i-1].start
		fmt.Printf("%d:%d\n", end-16384, ranges[i].start+16384)
	}
}

type rg struct {
	start int
	len   int
}
