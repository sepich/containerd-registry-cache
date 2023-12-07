package service

import "strconv"

type Non200Error struct {
	Code int
}

func (e *Non200Error) Error() string {
	return "Received non-200 code: " + strconv.Itoa(e.Code)
}

func (e *Non200Error) Is(tgt error) bool {
	_, ok := tgt.(*Non200Error)
	if !ok {
		return false
	}
	return true
}
