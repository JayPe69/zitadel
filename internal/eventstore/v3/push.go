package eventstore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/zitadel/logging"

	errs "github.com/zitadel/zitadel/internal/errors"
	"github.com/zitadel/zitadel/internal/eventstore"
)

func (es *Eventstore) Push(ctx context.Context, commands ...eventstore.Command) (_ []eventstore.Event, err error) {
	tx, err := es.client.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			txErr := tx.Rollback()
			logging.OnError(txErr).Debug("unable to rollback transaction")
			return
		}
		err = tx.Commit()
	}()
	sequences, err := latestSequences(ctx, tx, commands)
	if err != nil {
		return nil, err
	}

	events, err := insertEvents(ctx, tx, sequences, commands)
	if err != nil {
		return nil, err
	}

	if err = handleUniqueConstraints(ctx, tx, commands); err != nil {
		return nil, err
	}

	return events, nil
}

//go:embed push.sql
var pushStmt string

const maxRetries = 5

func insertEvents(ctx context.Context, tx *sql.Tx, sequences []*latestSequence, commands []eventstore.Command) ([]eventstore.Event, error) {
	events, placeHolders, args, err := mapCommands(commands, sequences)
	if err != nil {
		return nil, err
	}

	var rows *sql.Rows
	for i := 0; i < maxRetries; i++ {
		_, err = tx.ExecContext(ctx, "SAVEPOINT insert")
		if err != nil {
			return nil, errs.ThrowInternal(err, "V3-gd8jZ", "Errors.Internal")
		}
		rows, err = tx.QueryContext(ctx, fmt.Sprintf(pushStmt, strings.Join(placeHolders, ", ")), args...)
		if err != nil {
			logging.WithError(err).Debug("unable to insert")
			if errIsRetryable(err) {
				logging.WithError(err).Debug("retry tx")
				_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT insert")
				logging.OnError(err).Debug("rollback failed")
				continue
			}
			break
		}
		defer rows.Close()
		_, err = tx.ExecContext(ctx, "RELEASE SAVEPOINT insert")
		break
	}
	if err != nil {
		return nil, err
	}

	for i := 0; rows.Next(); i++ {
		err = rows.Scan(&events[i].(*event).createdAt)
		if err != nil {
			return nil, err
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

const argsPerCommand = 9

func mapCommands(commands []eventstore.Command, sequences []*latestSequence) (events []eventstore.Event, placeHolders []string, args []any, err error) {
	events = make([]eventstore.Event, len(commands))
	args = make([]any, 0, len(commands)*argsPerCommand)
	placeHolders = make([]string, len(commands))

	for i, command := range commands {
		sequence := searchSequenceByCommand(sequences, command)
		if sequence == nil {
			logging.WithFields(
				"aggType", command.Aggregate().Type,
				"aggID", command.Aggregate().ID,
				"instance", command.Aggregate().InstanceID,
			).Panic("no sequence found")
			// added return for linting
			return nil, nil, nil, nil
		}
		sequence.sequence++

		events[i], err = commandToEvent(sequence, command)
		if err != nil {
			return nil, nil, nil, err
		}

		placeHolders[i] = fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			i*argsPerCommand+1,
			i*argsPerCommand+2,
			i*argsPerCommand+3,
			i*argsPerCommand+4,
			i*argsPerCommand+5,
			i*argsPerCommand+6,
			i*argsPerCommand+7,
			i*argsPerCommand+8,
			i*argsPerCommand+9,
		)
		args = append(args,
			events[i].(*event).aggregate.InstanceID,
			events[i].(*event).aggregate.ResourceOwner,
			events[i].(*event).aggregate.Type,
			events[i].(*event).aggregate.ID,
			events[i].(*event).aggregate.Version,
			events[i].(*event).creator,
			events[i].(*event).typ,
			events[i].(*event).payload,
			events[i].(*event).sequence,
		)
	}

	return events, placeHolders, args, nil
}

func errIsRetryable(err error) bool {
	// We look for either:
	//  - the standard PG errcode SerializationFailureError:40001 or
	//  - the Cockroach extension errcode RetriableError:CR000. This extension
	//    has been removed server-side, but support for it has been left here for
	//    now to maintain backwards compatibility.
	code := errCode(err)
	return code == "CR000" || code == "40001"
}

func errCode(err error) string {
	var sqlErr errWithSQLState
	if errors.As(err, &sqlErr) {
		return sqlErr.SQLState()
	}

	return ""
}

// errWithSQLState is implemented by pgx (pgconn.PgError) and lib/pq
type errWithSQLState interface {
	SQLState() string
}