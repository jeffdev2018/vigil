package db

// Comment converts the sqlc row returned by the CTE-backed create query into
// the canonical model used by comment rendering and task side effects. The
// row's IssueRevision remains available separately for owner-cache coherence.
func (r CreateCommentRow) Comment() Comment {
	// A2aIntent (F19) rides here for the same reason as QuickActionID: the
	// create response and the realtime payload must expose the marker the row
	// was stamped with, and this hand-written projection is the only place that
	// carries it across.
	return Comment{
		ID:             r.ID,
		IssueID:        r.IssueID,
		AuthorType:     r.AuthorType,
		AuthorID:       r.AuthorID,
		Content:        r.Content,
		Type:           r.Type,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		ParentID:       r.ParentID,
		WorkspaceID:    r.WorkspaceID,
		ResolvedAt:     r.ResolvedAt,
		ResolvedByType: r.ResolvedByType,
		ResolvedByID:   r.ResolvedByID,
		SourceTaskID:   r.SourceTaskID,
		QuickActionID:  r.QuickActionID,
		A2aIntent:      r.A2aIntent,
		ViaPluginID:    r.ViaPluginID,
		Revision:       r.Revision,
		// Diff anchor (F07). Carried through so the create/update response and
		// the realtime payload expose the anchor the row was stamped with.
		AnchorKind:         r.AnchorKind,
		AnchorPrSource:     r.AnchorPrSource,
		AnchorPrID:         r.AnchorPrID,
		AnchorHeadSha:      r.AnchorHeadSha,
		AnchorFilePath:     r.AnchorFilePath,
		AnchorLineStart:    r.AnchorLineStart,
		AnchorLineEnd:      r.AnchorLineEnd,
		AnchorSide:         r.AnchorSide,
		AnchorReviewFlagID: r.AnchorReviewFlagID,
	}
}

// Comment converts the sqlc row returned by the CTE-backed update query into
// the canonical model. IssueRevision is aggregate-owner metadata and remains
// available on the row for response/event cache reconciliation.
func (r UpdateCommentRow) Comment() Comment {
	return Comment{
		ID:             r.ID,
		IssueID:        r.IssueID,
		AuthorType:     r.AuthorType,
		AuthorID:       r.AuthorID,
		Content:        r.Content,
		Type:           r.Type,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		ParentID:       r.ParentID,
		WorkspaceID:    r.WorkspaceID,
		ResolvedAt:     r.ResolvedAt,
		ResolvedByType: r.ResolvedByType,
		ResolvedByID:   r.ResolvedByID,
		SourceTaskID:   r.SourceTaskID,
		QuickActionID:  r.QuickActionID,
		A2aIntent:      r.A2aIntent,
		ViaPluginID:    r.ViaPluginID,
		Revision:       r.Revision,
		// Diff anchor (F07). Carried through so the create/update response and
		// the realtime payload expose the anchor the row was stamped with.
		AnchorKind:         r.AnchorKind,
		AnchorPrSource:     r.AnchorPrSource,
		AnchorPrID:         r.AnchorPrID,
		AnchorHeadSha:      r.AnchorHeadSha,
		AnchorFilePath:     r.AnchorFilePath,
		AnchorLineStart:    r.AnchorLineStart,
		AnchorLineEnd:      r.AnchorLineEnd,
		AnchorSide:         r.AnchorSide,
		AnchorReviewFlagID: r.AnchorReviewFlagID,
	}
}

// Comment converts one row of the anchored-threads read into the canonical
// model, so the same rendering path serves it as serves an ordinary list.
func (r ListAnchoredThreadsForPrRow) Comment() Comment {
	return Comment{
		ID:                 r.ID,
		IssueID:            r.IssueID,
		AuthorType:         r.AuthorType,
		AuthorID:           r.AuthorID,
		Content:            r.Content,
		Type:               r.Type,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
		ParentID:           r.ParentID,
		WorkspaceID:        r.WorkspaceID,
		ResolvedAt:         r.ResolvedAt,
		ResolvedByType:     r.ResolvedByType,
		ResolvedByID:       r.ResolvedByID,
		SourceTaskID:       r.SourceTaskID,
		QuickActionID:      r.QuickActionID,
		A2aIntent:          r.A2aIntent,
		ViaPluginID:        r.ViaPluginID,
		Revision:           r.Revision,
		AnchorKind:         r.AnchorKind,
		AnchorPrSource:     r.AnchorPrSource,
		AnchorPrID:         r.AnchorPrID,
		AnchorHeadSha:      r.AnchorHeadSha,
		AnchorFilePath:     r.AnchorFilePath,
		AnchorLineStart:    r.AnchorLineStart,
		AnchorLineEnd:      r.AnchorLineEnd,
		AnchorSide:         r.AnchorSide,
		AnchorReviewFlagID: r.AnchorReviewFlagID,
	}
}

// Comment converts a resolved thread root into the canonical model. Only the
// anchor columns are read from it, but returning the whole comment keeps the
// caller free of a second shape.
func (r ListAnchoredRootsForCommentsRow) Comment() Comment {
	return Comment{
		ID:                 r.ID,
		IssueID:            r.IssueID,
		AuthorType:         r.AuthorType,
		AuthorID:           r.AuthorID,
		Content:            r.Content,
		Type:               r.Type,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
		ParentID:           r.ParentID,
		WorkspaceID:        r.WorkspaceID,
		ResolvedAt:         r.ResolvedAt,
		ResolvedByType:     r.ResolvedByType,
		ResolvedByID:       r.ResolvedByID,
		SourceTaskID:       r.SourceTaskID,
		QuickActionID:      r.QuickActionID,
		A2aIntent:          r.A2aIntent,
		ViaPluginID:        r.ViaPluginID,
		Revision:           r.Revision,
		AnchorKind:         r.AnchorKind,
		AnchorPrSource:     r.AnchorPrSource,
		AnchorPrID:         r.AnchorPrID,
		AnchorHeadSha:      r.AnchorHeadSha,
		AnchorFilePath:     r.AnchorFilePath,
		AnchorLineStart:    r.AnchorLineStart,
		AnchorLineEnd:      r.AnchorLineEnd,
		AnchorSide:         r.AnchorSide,
		AnchorReviewFlagID: r.AnchorReviewFlagID,
	}
}
