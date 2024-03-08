package query

import (
	"context"
	"database/sql"
	"errors"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/zitadel/zitadel/internal/api/call"
	"github.com/zitadel/zitadel/internal/domain"
	"github.com/zitadel/zitadel/internal/query/projection"
	"github.com/zitadel/zitadel/internal/zerrors"
)

var (
	targetTable = table{
		name:          projection.TargetTable,
		instanceIDCol: projection.TargetInstanceIDCol,
	}
	TargetColumnID = Column{
		name:  projection.TargetIDCol,
		table: targetTable,
	}
	TargetColumnCreationDate = Column{
		name:  projection.TargetCreationDateCol,
		table: targetTable,
	}
	TargetColumnChangeDate = Column{
		name:  projection.TargetChangeDateCol,
		table: targetTable,
	}
	TargetColumnResourceOwner = Column{
		name:  projection.TargetResourceOwnerCol,
		table: targetTable,
	}
	TargetColumnInstanceID = Column{
		name:  projection.TargetInstanceIDCol,
		table: targetTable,
	}
	TargetColumnSequence = Column{
		name:  projection.TargetSequenceCol,
		table: targetTable,
	}
	TargetColumnName = Column{
		name:  projection.TargetNameCol,
		table: targetTable,
	}
	TargetColumnTargetType = Column{
		name:  projection.TargetTargetType,
		table: targetTable,
	}
	TargetColumnURL = Column{
		name:  projection.TargetURLCol,
		table: targetTable,
	}
	TargetColumnTimeout = Column{
		name:  projection.TargetTimeoutCol,
		table: targetTable,
	}
	TargetColumnAsync = Column{
		name:  projection.TargetAsyncCol,
		table: targetTable,
	}
	TargetColumnInterruptOnError = Column{
		name:  projection.TargetInterruptOnErrorCol,
		table: targetTable,
	}
)

type Targets struct {
	SearchResponse
	Targets []*Target
}

func (t *Targets) SetState(s *State) {
	t.State = s
}

type Target struct {
	ID string
	*domain.ObjectDetails

	Name             string
	TargetType       domain.TargetType
	URL              string
	timeout          time.Duration
	Async            bool
	InterruptOnError bool
}

func (a *Target) Timeout() time.Duration {
	if a.timeout > 0 && a.timeout < maxTimeout {
		return a.timeout
	}
	return maxTimeout
}

type TargetSearchQueries struct {
	SearchRequest
	Queries []SearchQuery
}

func (q *TargetSearchQueries) toQuery(query sq.SelectBuilder) sq.SelectBuilder {
	query = q.SearchRequest.toQuery(query)
	for _, q := range q.Queries {
		query = q.toQuery(query)
	}
	return query
}

func (q *Queries) SearchTargets(ctx context.Context, queries *TargetSearchQueries, resourceOwner string) (targets *Targets, err error) {
	eq := sq.Eq{
		TargetColumnInstanceID.identifier():    resourceOwner,
		TargetColumnResourceOwner.identifier(): resourceOwner,
	}
	return genericSearch[*Targets](q, ctx, targetTable, prepareTargetsQuery, whereWrapper(queries.toQuery, eq))
}

func (q *Queries) GetTargetByID(ctx context.Context, id string, resourceOwner string) (target *Target, err error) {
	eq := sq.Eq{
		TargetColumnID.identifier():            id,
		TargetColumnResourceOwner.identifier(): resourceOwner,
		TargetColumnInstanceID.identifier():    resourceOwner,
	}
	return genericGetByID[*Target](q, ctx, prepareTargetQuery, where(eq))
}

func NewTargetNameSearchQuery(method TextComparison, value string) (SearchQuery, error) {
	return NewTextQuery(TargetColumnName, value, method)
}

func NewTargetInIDsSearchQuery(values []string) (SearchQuery, error) {
	return NewInTextQuery(TargetColumnID, values)
}

func prepareTargetsQuery(ctx context.Context, db prepareDatabase) (sq.SelectBuilder, func(rows *sql.Rows) (*Targets, error)) {
	return sq.Select(
			TargetColumnID.identifier(),
			TargetColumnChangeDate.identifier(),
			TargetColumnResourceOwner.identifier(),
			TargetColumnSequence.identifier(),
			TargetColumnName.identifier(),
			TargetColumnTargetType.identifier(),
			TargetColumnTimeout.identifier(),
			TargetColumnURL.identifier(),
			TargetColumnAsync.identifier(),
			TargetColumnInterruptOnError.identifier(),
			countColumn.identifier(),
		).From(targetTable.identifier() + db.Timetravel(call.Took(ctx))).
			PlaceholderFormat(sq.Dollar),
		func(rows *sql.Rows) (*Targets, error) {
			targets := make([]*Target, 0)
			var count uint64
			for rows.Next() {
				target := new(Target)
				target.ObjectDetails = new(domain.ObjectDetails)
				err := rows.Scan(
					&target.ID,
					&target.EventDate,
					&target.ResourceOwner,
					&target.Sequence,
					&target.Name,
					&target.TargetType,
					&target.timeout,
					&target.URL,
					&target.Async,
					&target.InterruptOnError,
					&count,
				)
				if err != nil {
					return nil, err
				}
				targets = append(targets, target)
			}

			if err := rows.Close(); err != nil {
				return nil, zerrors.ThrowInternal(err, "QUERY-fzwi6cgxos", "Errors.Query.CloseRows")
			}

			return &Targets{
				Targets: targets,
				SearchResponse: SearchResponse{
					Count: count,
				},
			}, nil
		}
}

func prepareTargetQuery(ctx context.Context, db prepareDatabase) (sq.SelectBuilder, func(row *sql.Row) (*Target, error)) {
	return sq.Select(
			TargetColumnID.identifier(),
			TargetColumnChangeDate.identifier(),
			TargetColumnResourceOwner.identifier(),
			TargetColumnSequence.identifier(),
			TargetColumnName.identifier(),
			TargetColumnTargetType.identifier(),
			TargetColumnTimeout.identifier(),
			TargetColumnURL.identifier(),
			TargetColumnAsync.identifier(),
			TargetColumnInterruptOnError.identifier(),
		).From(targetTable.identifier() + db.Timetravel(call.Took(ctx))).
			PlaceholderFormat(sq.Dollar),
		func(row *sql.Row) (*Target, error) {
			target := new(Target)
			target.ObjectDetails = new(domain.ObjectDetails)
			err := row.Scan(
				&target.ID,
				&target.EventDate,
				&target.ResourceOwner,
				&target.Sequence,
				&target.Name,
				&target.TargetType,
				&target.timeout,
				&target.URL,
				&target.Async,
				&target.InterruptOnError,
			)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, zerrors.ThrowNotFound(err, "QUERY-hj5oaniyrz", "Errors.Target.NotFound")
				}
				return nil, zerrors.ThrowInternal(err, "QUERY-5qhc19sc49", "Errors.Internal")
			}
			return target, nil
		}
}