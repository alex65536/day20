package webui

type progressPartData struct {
	Has        bool
	Value      float64
	Percentage float64
}

func buildProgressPartData(done, count int64) *progressPartData {
	if count <= 0 {
		return &progressPartData{Has: false}
	}
	val := float64(done) / float64(count)
	return &progressPartData{
		Has:        true,
		Value:      val,
		Percentage: val * 100,
	}
}
