package registry

import (
	"gerrit-share.lan/go/actors"
	"gerrit-share.lan/go/inspect"
)

type infoStateChangeStream struct {
	buffer            InfoArray
	names             map[string]actors.ActorService
	startOffset       int
	isAnyoneListening bool
}

func (i *infoStateChangeStream) Add(name string, actor actors.ActorService) {
	i.names[name] = actor
	if !i.isAnyoneListening {
		return
	}
	i.buffer = append(i.buffer, Info{name, actor})
}

func (i *infoStateChangeStream) Get(name string) (actors.ActorService, bool) {
	result, ok := i.names[name]
	return result, ok
}

func (i *infoStateChangeStream) Contains(name string) bool {
	_, ok := i.names[name]
	return ok
}

func (i *infoStateChangeStream) Remove(name string) {
	delete(i.names, name)
}

func (i *infoStateChangeStream) fillArray(array *InfoArray, offset int, maxLen int) (inspect.Inspectable, int, error) {
	realLen := len(i.buffer) - offset + i.startOffset
	if realLen > maxLen {
		realLen = maxLen
	}
	if realLen == 0 {
		return nil, offset, nil
	}
	array.SetLength(realLen)
	nextOffset := offset + copy(*array, i.buffer[(offset-i.startOffset):])
	return array, nextOffset, nil
}

func (i *infoStateChangeStream) FillData(data inspect.Inspectable, offset int, maxLen int) (result inspect.Inspectable, nextOffset int, err error) {
	if offset < i.startOffset {
		return data, offset, actors.ErrOffsetOutOfRange
	}
	if maxLen == 0 {
		maxLen = actors.DefaultMaxLen
	}
	if array, ok := data.(*InfoArray); ok {
		return i.fillArray(array, offset, maxLen)
	} else if value, ok := data.(*Info); ok {
		if (offset - i.startOffset) > len(i.buffer) {
			return nil, offset, actors.ErrOffsetOutOfRange
		}
		if (offset - i.startOffset) == len(i.buffer) {
			return nil, offset, nil
		}
		*value = i.buffer[offset-i.startOffset]
		return value, offset + 1, nil
	} else {
		return i.fillArray(new(InfoArray), offset, maxLen)
	}
	return data, offset, actors.ErrWrongTypeRequested
}

func (i *infoStateChangeStream) GetLatestState() (int, actors.DataSource) {
	array := make(InfoArray, 0, len(i.names))
	for name, actor := range i.names {
		array = append(array, Info{name, actor})
	}
	i.isAnyoneListening = true //start collecting history
	return i.startOffset + len(i.buffer), &staticInfoDataSource{Data: array}
}

func (i *infoStateChangeStream) LastOffsetChanged(offset int) {
	if len(i.buffer)/2 >= (offset - i.startOffset) {
		var buffer InfoArray
		buffer.SetLength(len(i.buffer) + i.startOffset - offset)
		copy(buffer, i.buffer[offset-i.startOffset:])
		i.buffer = buffer
		i.startOffset = offset
	}
}

func (i *infoStateChangeStream) NoMoreSubscribers() {
	i.buffer.SetLength(0)
	i.startOffset = 0
	i.isAnyoneListening = false
}

func init() {
	var _ actors.StateChangeStream = (*infoStateChangeStream)(nil)
}