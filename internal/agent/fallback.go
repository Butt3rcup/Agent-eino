package agent

type FallbackController struct {
	mapping map[string]string
}

func NewFallbackController() *FallbackController {
	return &FallbackController{mapping: map[string]string{
		ModeRAG:        ModeRAGAgent,
		ModeRAGAgent:   ModeRAG,
		ModeMultiAgent: ModeRAGAgent,
		ModeGraphMulti: ModeMultiAgent,
		ModeGraphRAG:   ModeRAG,
		ModeReact:      ModeRAG,
	}}
}

func (f *FallbackController) NextMode(mode string) (string, bool) {
	if f == nil {
		return "", false
	}
	next, ok := f.mapping[normalizeMode(mode)]
	return next, ok
}
