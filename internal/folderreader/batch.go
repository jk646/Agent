package folderreader

import (
	"context"
	"fmt"
)

func (m *Manager) Batch(ctx context.Context, params BatchParams) (BatchResult, error) {
	if len(params.Reads) == 0 || len(params.Reads) > m.cfg.MaxBatchItems {
		return BatchResult{}, fmt.Errorf("%w: reads must contain 1 to %d items", ErrInvalidRequest, m.cfg.MaxBatchItems)
	}
	result := BatchResult{Results: make([]BatchItem, 0, len(params.Reads))}
	for _, request := range params.Reads {
		switch request.Kind {
		case "stat":
			if request.Stat == nil {
				return BatchResult{}, fmt.Errorf("%w: stat parameters are required", ErrInvalidRequest)
			}
			item, err := m.Stat(ctx, *request.Stat)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "stat", Stat: &item})
		case "list":
			if request.List == nil {
				return BatchResult{}, fmt.Errorf("%w: list parameters are required", ErrInvalidRequest)
			}
			item, err := m.List(ctx, *request.List)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "list", List: &item})
		case "tree":
			if request.Tree == nil {
				return BatchResult{}, fmt.Errorf("%w: tree parameters are required", ErrInvalidRequest)
			}
			item, err := m.Tree(ctx, *request.Tree)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "tree", Tree: &item})
		case "summary":
			if request.Summary == nil {
				return BatchResult{}, fmt.Errorf("%w: summary parameters are required", ErrInvalidRequest)
			}
			item, err := m.Summary(ctx, *request.Summary)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "summary", Summary: &item})
		case "snapshot":
			if request.Snapshot == nil {
				return BatchResult{}, fmt.Errorf("%w: snapshot parameters are required", ErrInvalidRequest)
			}
			item, err := m.Snapshot(ctx, *request.Snapshot)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "snapshot", Snapshot: &item})
		case "compare":
			if request.Compare == nil {
				return BatchResult{}, fmt.Errorf("%w: compare parameters are required", ErrInvalidRequest)
			}
			item, err := m.Compare(ctx, *request.Compare)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "compare", Compare: &item})
		default:
			return BatchResult{}, fmt.Errorf("%w: unsupported folder read kind %q", ErrInvalidRequest, request.Kind)
		}
	}
	return result, nil
}
