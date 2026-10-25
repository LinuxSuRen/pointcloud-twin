package tiler

// tilesetJSON 及其子结构对应 3D Tiles 规范中的 tileset JSON。
type tilesetJSON struct {
	Asset struct {
		Version   string `json:"version"`
		Generator string `json:"generator"`
	} `json:"asset"`
	GeometricError float64   `json:"geometricError"`
	Root           *tileJSON `json:"root"`
}

type tileJSON struct {
	Transform      [16]float64    `json:"transform"`
	BoundingVolume boundingVolume `json:"boundingVolume"`
	Refine         string         `json:"refine,omitempty"`
	GeometricError float64        `json:"geometricError"`
	Content        *contentJSON   `json:"content,omitempty"`
	Children       []*tileJSON    `json:"children,omitempty"`
}

type boundingVolume struct {
	Box *[12]float64 `json:"box"`
}

type contentJSON struct {
	URI string `json:"uri"`
}

func newTileset(version string) *tilesetJSON {
	ts := &tilesetJSON{}
	ts.Asset.Version = version
	ts.Asset.Generator = generator
	return ts
}
