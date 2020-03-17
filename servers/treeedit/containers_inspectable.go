package treeedit

import (
	"gerrit-share.lan/go/inspect"
)

type Divisions map[string]*Division

func (o *Divisions) Inspect(i *inspect.GenericInspector) {
	mapInspector := i.Map("", "", "division list")
	if mapInspector.IsReading() {
		length := mapInspector.GetLength()
		if *o == nil {
			*o = make(Divisions, length)
		}
		for i := 0; i < length; i++ {
			obj := NewDivision("")
			key := mapInspector.NextKey()
			obj.Inspect(mapInspector.ReadValue())
			(*o)[key] = obj
		}
	} else {
		mapInspector.SetLength(len(*o))
		for key, value := range *o {
			valI := mapInspector.WriteValue(key)
			value.Inspect(valI)
		}
	}
	mapInspector.End()
}

type Positions map[string]*Position

func (o *Positions) Inspect(i *inspect.GenericInspector) {
	mapInspector := i.Map("", "", "position list")
	if mapInspector.IsReading() {
		length := mapInspector.GetLength()
		if *o == nil {
			*o = make(Positions, length)
		}
		for i := 0; i < length; i++ {
			obj := NewPosition(false, "", "")
			key := mapInspector.NextKey()
			obj.Inspect(mapInspector.ReadValue())
			(*o)[key] = obj
		}
	} else {
		mapInspector.SetLength(len(*o))
		for key, value := range *o {
			valI := mapInspector.WriteValue(key)
			value.Inspect(valI)
		}
	}
	mapInspector.End()
}

type division1Array []Division1

func (d *division1Array) Inspect(i *inspect.GenericInspector) {
	arrayInspector := i.Array("", "", "")
	if !arrayInspector.IsReading() {
		arrayInspector.SetLength(len(*d))
	} else {
		*d = make(division1Array, arrayInspector.GetLength())
	}
	for index := range *d {
		(*d)[index].Inspect(arrayInspector.Value())
	}
	arrayInspector.End()
}

type positionArray []Position

func (p *positionArray) Inspect(i *inspect.GenericInspector) {
	arrayInspector := i.Array("", "", "")
	if !arrayInspector.IsReading() {
		arrayInspector.SetLength(len(*p))
	} else {
		*p = make(positionArray, arrayInspector.GetLength())
	}
	for index := range *p {
		(*p)[index].Inspect(arrayInspector.Value())
	}
	arrayInspector.End()
}