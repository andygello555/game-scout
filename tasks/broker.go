package tasks

import (
	"context"
	"github.com/RichardKnop/machinery/example/tracers"
	"github.com/RichardKnop/machinery/v1"
	"github.com/RichardKnop/machinery/v1/backends/result"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/google/uuid"
	"github.com/opentracing/opentracing-go"
	opentracinglog "github.com/opentracing/opentracing-go/log"
)

type Broker struct {
	CleanupMachinery func()
	CleanupSpan      func()
	BatchID          string
	Server           *machinery.Server
	Ctx              context.Context
}

// Cleanup cleans up the machinery server and the span that were created for this Broker.
func (b *Broker) Cleanup() {
	b.CleanupMachinery()
	b.CleanupSpan()
}

// SendTaskWithContext acts as a wrapper for machinery.Server.SendTaskWithContext.
func (b *Broker) SendTaskWithContext(signature *tasks.Signature) (*result.AsyncResult, error) {
	return b.Server.SendTaskWithContext(b.Ctx, signature)
}

// SendGroupWithContext acts as a wrapper for machinery.Server.SendGroupWithContext
func (b *Broker) SendGroupWithContext(group *tasks.Group, sendConcurrency int) ([]*result.AsyncResult, error) {
	return b.Server.SendGroupWithContext(b.Ctx, group, sendConcurrency)
}

// SendChordWithContext acts as a wrapper for machinery.Server.SendChordWithContext
func (b *Broker) SendChordWithContext(chord *tasks.Chord, sendConcurrency int) (*result.ChordAsyncResult, error) {
	return b.Server.SendChordWithContext(b.Ctx, chord, sendConcurrency)
}

// SendChainWithContext acts as a wrapper for machinery.Server.SendChainWithContext
func (b *Broker) SendChainWithContext(chain *tasks.Chain) (*result.ChainAsyncResult, error) {
	return b.Server.SendChainWithContext(b.Ctx, chain)
}

func setupMachineryServer(config Config) (cleanup func(), server *machinery.Server, err error) {
	if cleanup, err = tracers.SetupTracer("sender"); err != nil {
		log.FATAL.Fatalln("Unable to instantiate a tracer:", err)
	}

	if server, err = StartServer(config); err != nil {
		return nil, nil, err
	}
	return cleanup, server, nil
}

// startSpan starts a span representing this run of the `send` command and set a batch id as baggage so it can travel
// all the way into the worker functions.
func startSpan() (finish func(), batchID string, ctx context.Context) {
	var span opentracing.Span
	span, ctx = opentracing.StartSpanFromContext(context.Background(), "send")

	batchID = uuid.New().String()
	span.SetBaggageItem("batch.id", batchID)
	span.LogFields(opentracinglog.String("batch.id", batchID))
	return span.Finish, batchID, ctx
}

func NewBroker(config Config) (broker *Broker, err error) {
	broker = &Broker{}
	if broker.CleanupMachinery, broker.Server, err = setupMachineryServer(config); err != nil {
		return nil, err
	}
	broker.CleanupSpan, broker.BatchID, broker.Ctx = startSpan()
	return broker, nil
}
