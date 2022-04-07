package rardecode

// ErrRange shows invalid data block in file
type ErrRange struct {
	File       string
	Start, End int64
	err        error
}

func (r *ErrRange) Error() string {
	return r.err.Error()
}

func rangeErr(file string, start int64) *ErrRange {
	return &ErrRange{
		File:  file,
		Start: start,
	}
}

func (r *ErrRange) fill(end int64, err error) error {
	r.End = end
	r.err = err
	return r
}
