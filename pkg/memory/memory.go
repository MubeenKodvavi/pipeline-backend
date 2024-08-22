package memory

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/instill-ai/pipeline-backend/pkg/constant"
	"github.com/instill-ai/pipeline-backend/pkg/data"
	"github.com/instill-ai/pipeline-backend/pkg/datamodel"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
)

type PipelineDataType string
type ComponentStatusType string
type ComponentDataType string

const (
	PipelineVariable PipelineDataType = "variable"
	PipelineSecret   PipelineDataType = "secret"
	PipelineOutput   PipelineDataType = "output"

	// We preserve the `PipelineOutputTemplate` in memory to re-render the
	// results.
	PipelineOutputTemplate PipelineDataType = "_output"
)

const (
	ComponentStatusStarted   ComponentStatusType = "started"
	ComponentStatusSkipped   ComponentStatusType = "skipped"
	ComponentStatusCompleted ComponentStatusType = "completed"
)

const (
	ComponentDataInput   ComponentDataType = "input"
	ComponentDataOutput  ComponentDataType = "output"
	ComponentDataElement ComponentDataType = "element"
	ComponentDataSetup   ComponentDataType = "setup"
)

type MemoryStore interface {
	NewWorkflowMemory(ctx context.Context, workflowID string, recipe *datamodel.Recipe, batchSize int) (workflow WorkflowMemory, err error)
	GetWorkflowMemory(ctx context.Context, workflowID string) (workflow WorkflowMemory, err error)
	PurgeWorkflowMemory(ctx context.Context, workflowID string) (err error)

	WriteWorkflowMemoryToRedis(ctx context.Context, workflowID string) (err error)
	LoadWorkflowMemoryFromRedis(ctx context.Context, workflowID string) (workflow WorkflowMemory, err error)

	SendWorkflowStatusEvent(ctx context.Context, workflowID string, event Event) (err error)
}

type WorkflowMemory interface {
	Set(ctx context.Context, batchIdx int, key string, value data.Value) (err error)
	Get(ctx context.Context, batchIdx int, path string) (value data.Value, err error)

	InitComponent(ctx context.Context, batchIdx int, componentID string)
	SetComponentData(ctx context.Context, batchIdx int, componentID string, t ComponentDataType, value data.Value) (err error)
	GetComponentData(ctx context.Context, batchIdx int, componentID string, t ComponentDataType) (value data.Value, err error)
	SetComponentStatus(ctx context.Context, batchIdx int, componentID string, t ComponentStatusType, value bool) (err error)
	GetComponentStatus(ctx context.Context, batchIdx int, componentID string, t ComponentStatusType) (value bool, err error)
	SetPipelineData(ctx context.Context, batchIdx int, t PipelineDataType, value data.Value) (err error)
	GetPipelineData(ctx context.Context, batchIdx int, t PipelineDataType) (value data.Value, err error)

	EnableStreaming()

	GetBatchSize() int
	SetRecipe(*datamodel.Recipe)
	GetRecipe() *datamodel.Recipe
}

type ComponentStatus struct {
	Started   bool `json:"started"`
	Completed bool `json:"completed"`
	Skipped   bool `json:"skipped"`
}

type memoryStore struct {
	workflows   sync.Map
	redisClient *redis.Client
}

type workflowMemory struct {
	mu          sync.Mutex
	ID          string
	Data        []data.Value
	Recipe      *datamodel.Recipe
	redisClient *redis.Client
	streaming   bool
}

type EventType string
type Event struct {
	Event EventType `json:"event"`
	Data  any       `json:"data"`
}

type PipelineStartedEventData struct {
	UpdateTime time.Time `json:"updateTime"`
	BatchIndex int       `json:"batchIndex"`
	Variable   any       `json:"variable"`
}

type PipelineCompletedEventData struct {
	UpdateTime time.Time `json:"updateTime"`
	BatchIndex int       `json:"batchIndex"`
	Output     any       `json:"output"`
}

type ComponentStatusEventData struct {
	UpdateTime  time.Time                    `json:"updateTime"`
	ComponentID string                       `json:"componentID"`
	BatchIndex  int                          `json:"batchIndex"`
	Status      map[ComponentStatusType]bool `json:"status"`
}

type ComponentInputEventData struct {
	UpdateTime  time.Time `json:"updateTime"`
	ComponentID string    `json:"componentID"`
	BatchIndex  int       `json:"batchIndex"`
	Input       any       `json:"input"`
}
type ComponentOutputEventData struct {
	UpdateTime  time.Time `json:"updateTime"`
	ComponentID string    `json:"componentID"`
	BatchIndex  int       `json:"batchIndex"`
	Output      any       `json:"output"`
}

const (
	PipelineStarted        EventType = "pipeline_started"
	PipelineOutputUpdated  EventType = "pipeline_output_updated"
	PipelineCompleted      EventType = "pipeline_completed"
	PipelineClosed         EventType = "pipeline_closed"
	ComponentStatusUpdated EventType = "component_status_updated"
	ComponentInputUpdated  EventType = "component_input_updated"
	ComponentOutputUpdated EventType = "component_output_updated"
)

func init() {
	gob.Register(ComponentStatusEventData{})
	gob.Register(ComponentInputEventData{})
	gob.Register(ComponentOutputEventData{})
	gob.Register(PipelineStartedEventData{})
	gob.Register(PipelineCompletedEventData{})
}

func NewMemoryStore(rc *redis.Client) MemoryStore {
	return &memoryStore{
		workflows:   sync.Map{},
		redisClient: rc,
	}
}

func (ms *memoryStore) NewWorkflowMemory(ctx context.Context, workflowID string, r *datamodel.Recipe, batchSize int) (workflow WorkflowMemory, err error) {
	wfmData := make([]data.Value, batchSize)
	for idx := range batchSize {
		m := data.NewMap(map[string]data.Value{
			string(PipelineVariable): data.NewMap(nil),
			string(PipelineSecret):   data.NewMap(nil),
			string(PipelineOutput):   data.NewMap(nil),
		})

		wfmData[idx] = m
	}

	ms.workflows.Store(workflowID, &workflowMemory{
		mu:          sync.Mutex{},
		ID:          workflowID,
		Data:        wfmData,
		Recipe:      r,
		redisClient: ms.redisClient,
	})

	wfm, ok := ms.workflows.Load(workflowID)
	if !ok {
		return nil, fmt.Errorf("workflow memory not found")
	}

	return wfm.(WorkflowMemory), nil
}

func (ms *memoryStore) GetWorkflowMemory(ctx context.Context, workflowID string) (workflow WorkflowMemory, err error) {
	wfm, ok := ms.workflows.Load(workflowID)
	if !ok {
		return nil, fmt.Errorf("workflow memory not found")
	}

	return wfm.(WorkflowMemory), nil
}

func (ms *memoryStore) PurgeWorkflowMemory(ctx context.Context, workflowID string) (err error) {
	ms.workflows.Delete(workflowID)
	return nil
}

func (ms *memoryStore) SendWorkflowStatusEvent(ctx context.Context, workflowID string, event Event) (err error) {
	buf := bytes.Buffer{}
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(event)
	if err != nil {
		return err
	}
	err = ms.redisClient.Publish(ctx, workflowID, buf.Bytes()).Err()
	if err != nil {
		return err
	}

	return nil
}

func (ms *memoryStore) WriteWorkflowMemoryToRedis(ctx context.Context, workflowID string) (err error) {

	wfm, ok := ms.workflows.Load(workflowID)
	if !ok {
		return fmt.Errorf("workflow memory not found")
	}

	buf := bytes.Buffer{}
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(wfm)
	if err != nil {
		return err
	}
	cmd := ms.redisClient.Set(ctx, fmt.Sprintf("pipeline_trigger:%s", workflowID), buf.Bytes(), 1*time.Hour)
	if cmd.Err() != nil {
		return cmd.Err()
	}

	return nil
}

func (ms *memoryStore) LoadWorkflowMemoryFromRedis(ctx context.Context, workflowID string) (workflow WorkflowMemory, err error) {

	cmd := ms.redisClient.Get(ctx, fmt.Sprintf("pipeline_trigger:%s", workflowID))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	wfm := workflowMemory{}
	b, err := cmd.Bytes()
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(b)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&wfm)
	if err != nil {
		return nil, err
	}
	wfm.mu = sync.Mutex{}
	wfm.redisClient = ms.redisClient
	wfm.ID = workflowID
	ms.workflows.Store(workflowID, &wfm)
	return &wfm, nil

}
func (wfm *workflowMemory) EnableStreaming() {
	wfm.streaming = true
}
func (wfm *workflowMemory) InitComponent(ctx context.Context, batchIdx int, componentID string) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	compMemory := data.NewMap(
		map[string]data.Value{
			constant.SegInput:  data.NewMap(nil),
			constant.SegOutput: data.NewMap(nil),
			"status": data.NewMap(
				map[string]data.Value{
					"started":   data.NewBoolean(false),
					"skipped":   data.NewBoolean(false),
					"completed": data.NewBoolean(false),
				},
			),
			"setup": data.NewMap(nil),
		},
	)
	wfm.Data[batchIdx].(*data.Map).Fields[componentID] = compMemory
}

func (wfm *workflowMemory) SetComponentData(ctx context.Context, batchIdx int, componentID string, t ComponentDataType, value data.Value) (err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	if _, ok := wfm.Data[batchIdx].(*data.Map).Fields[componentID]; !ok {
		return fmt.Errorf("component %s not exist", componentID)
	}
	wfm.Data[batchIdx].(*data.Map).Fields[componentID].(*data.Map).Fields[string(t)] = value

	if wfm.streaming {
		// TODO: simplify struct conversion
		s, err := value.ToStructValue()
		if err != nil {
			return err
		}
		b, err := protojson.Marshal(s)
		if err != nil {
			return err
		}
		var data map[string]any
		err = json.Unmarshal(b, &data)
		if err != nil {
			return err
		}

		event := Event{}
		if t == ComponentDataInput {
			event.Event = ComponentInputUpdated
			event.Data = ComponentInputEventData{
				UpdateTime:  time.Now(),
				ComponentID: componentID,
				Input:       data,
			}
		} else if t == ComponentDataOutput {
			event.Event = ComponentOutputUpdated
			event.Data = ComponentOutputEventData{
				UpdateTime:  time.Now(),
				ComponentID: componentID,
				Output:      data,
			}
		}
		buf := bytes.Buffer{}
		enc := gob.NewEncoder(&buf)
		err = enc.Encode(event)
		if err != nil {
			return err
		}

		err = wfm.redisClient.Publish(ctx, wfm.ID, buf.Bytes()).Err()
		if err != nil {
			return err
		}

	}
	return nil
}
func (wfm *workflowMemory) GetComponentData(ctx context.Context, batchIdx int, componentID string, t ComponentDataType) (value data.Value, err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	if _, ok := wfm.Data[batchIdx].(*data.Map).Fields[componentID]; !ok {
		return nil, fmt.Errorf("component %s not exist", componentID)
	}
	return wfm.Data[batchIdx].(*data.Map).Fields[componentID].(*data.Map).Fields[string(t)], nil
}

func (wfm *workflowMemory) SetComponentStatus(ctx context.Context, batchIdx int, componentID string, t ComponentStatusType, value bool) (err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	if _, ok := wfm.Data[batchIdx].(*data.Map).Fields[componentID]; !ok {
		return fmt.Errorf("component %s not exist", componentID)
	}
	wfm.Data[batchIdx].(*data.Map).Fields[componentID].(*data.Map).Fields["status"].(*data.Map).Fields[string(t)] = data.NewBoolean(value)

	if wfm.streaming {
		// TODO: simplify struct conversion
		st := wfm.Data[batchIdx].(*data.Map).Fields[componentID].(*data.Map).Fields["status"].(*data.Map)
		started := st.Fields[string(ComponentStatusStarted)].(*data.Boolean).GetBoolean()
		skipped := st.Fields[string(ComponentStatusSkipped)].(*data.Boolean).GetBoolean()
		completed := st.Fields[string(ComponentStatusCompleted)].(*data.Boolean).GetBoolean()
		event := Event{
			Event: ComponentStatusUpdated,
			Data: ComponentStatusEventData{
				UpdateTime:  time.Now(),
				ComponentID: componentID,
				BatchIndex:  batchIdx,
				Status: map[ComponentStatusType]bool{
					ComponentStatusStarted:   started,
					ComponentStatusSkipped:   skipped,
					ComponentStatusCompleted: completed,
				},
			},
		}
		buf := bytes.Buffer{}
		enc := gob.NewEncoder(&buf)
		err = enc.Encode(event)
		if err != nil {
			return err
		}

		err = wfm.redisClient.Publish(ctx, wfm.ID, buf.Bytes()).Err()
		if err != nil {
			return err
		}
	}
	return err
}
func (wfm *workflowMemory) GetComponentStatus(ctx context.Context, batchIdx int, componentID string, t ComponentStatusType) (value bool, err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	if _, ok := wfm.Data[batchIdx].(*data.Map).Fields[componentID]; !ok {
		return false, fmt.Errorf("component %s not exist", componentID)
	}
	return wfm.Data[batchIdx].(*data.Map).Fields[componentID].(*data.Map).Fields["status"].(*data.Map).Fields[string(t)].(*data.Boolean).GetBoolean(), nil
}

func (wfm *workflowMemory) SetPipelineData(ctx context.Context, batchIdx int, t PipelineDataType, value data.Value) (err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	wfm.Data[batchIdx].(*data.Map).Fields[string(t)] = value

	if wfm.streaming {
		// TODO: simplify struct conversion
		s, err := value.ToStructValue()
		if err != nil {
			return err
		}
		b, err := protojson.Marshal(s)
		if err != nil {
			return err
		}
		var data map[string]any
		err = json.Unmarshal(b, &data)
		if err != nil {
			return err
		}
		event := Event{}
		if t == PipelineOutput {
			event.Event = PipelineOutputUpdated
			event.Data = PipelineCompletedEventData{
				UpdateTime: time.Now(),
				Output:     data,
			}
		}
		buf := bytes.Buffer{}
		enc := gob.NewEncoder(&buf)
		err = enc.Encode(event)
		if err != nil {
			return err
		}

		err = wfm.redisClient.Publish(ctx, wfm.ID, buf.Bytes()).Err()
		if err != nil {
			return err
		}
	}

	return nil
}

func (wfm *workflowMemory) GetPipelineData(ctx context.Context, batchIdx int, t PipelineDataType) (value data.Value, err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	if v, ok := wfm.Data[batchIdx].(*data.Map).Fields[string(t)]; !ok {
		return nil, fmt.Errorf("%s not exist", string(t))
	} else {
		return v, nil
	}
}

func (wfm *workflowMemory) Set(ctx context.Context, batchIdx int, key string, value data.Value) (err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	wfm.Data[batchIdx].(*data.Map).Fields[key] = value
	return nil
}

func (wfm *workflowMemory) Get(ctx context.Context, batchIdx int, path string) (memory data.Value, err error) {
	wfm.mu.Lock()
	defer wfm.mu.Unlock()

	if path == "" {
		return wfm.Data[batchIdx], nil
	}
	splits := strings.FieldsFunc(path, func(s rune) bool {
		return s == '.' || s == '['
	})

	newPath := ""
	for _, split := range splits {
		if strings.HasSuffix(split, "]") {
			// Array Index
			newPath += fmt.Sprintf("[%s", split)
		} else {
			// Map Key
			newPath += fmt.Sprintf("[\"%s\"]", split)
		}
	}

	newPath = newPath[1 : len(newPath)-1]
	ss := strings.Split(newPath, "][")
	ptr := wfm.Data[batchIdx]
	for _, s := range ss {
		if i, err := strconv.Atoi(s); err == nil {
			ptr = ptr.(*data.Array).Values[i]
		} else {
			key := s[1 : len(s)-1]
			ptr = ptr.(*data.Map).Fields[key]
		}
	}

	return ptr, nil
}

func (wfm *workflowMemory) GetBatchSize() int {
	return len(wfm.Data)
}

func (wfm *workflowMemory) SetRecipe(r *datamodel.Recipe) {
	wfm.Recipe = r
}

func (wfm *workflowMemory) GetRecipe() *datamodel.Recipe {
	return wfm.Recipe
}
