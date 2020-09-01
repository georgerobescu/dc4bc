package client

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	sipf "github.com/depools/dc4bc/fsm/state_machines/signing_proposal_fsm"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/depools/dc4bc/airgapped"
	"github.com/depools/dc4bc/client/types"
	"github.com/depools/dc4bc/fsm/types/requests"
	"github.com/google/uuid"

	"github.com/depools/dc4bc/fsm/state_machines/signature_proposal_fsm"
	spf "github.com/depools/dc4bc/fsm/state_machines/signature_proposal_fsm"

	"github.com/depools/dc4bc/fsm/state_machines"

	"github.com/depools/dc4bc/fsm/fsm"
	dpf "github.com/depools/dc4bc/fsm/state_machines/dkg_proposal_fsm"
	"github.com/depools/dc4bc/qr"
	"github.com/depools/dc4bc/storage"
)

const (
	pollingPeriod = time.Second
	QrCodesDir    = "/tmp"
)

type Client struct {
	sync.Mutex
	logger      *logger
	userName    string
	address     string
	pubKey      ed25519.PublicKey
	ctx         context.Context
	state       State
	storage     storage.Storage
	keyStore    KeyStore
	qrProcessor qr.Processor
	Airgapped   *airgapped.AirgappedMachine
}

func NewClient(
	ctx context.Context,
	userName string,
	state State,
	storage storage.Storage,
	keyStore KeyStore,
	qrProcessor qr.Processor,
	airgappedMachine *airgapped.AirgappedMachine,
) (*Client, error) {
	keyPair, err := keyStore.LoadKeys(userName, "")
	if err != nil {
		return nil, fmt.Errorf("failed to LoadKeys: %w", err)
	}

	return &Client{
		ctx:         ctx,
		logger:      newLogger(userName),
		userName:    userName,
		address:     keyPair.GetAddr(),
		pubKey:      keyPair.Pub,
		state:       state,
		storage:     storage,
		keyStore:    keyStore,
		qrProcessor: qrProcessor,
		Airgapped:   airgappedMachine,
	}, nil
}

func (c *Client) GetAddr() string {
	return c.address
}

func (c *Client) GetPubKey() ed25519.PublicKey {
	return c.pubKey
}

func (c *Client) Poll() error {
	tk := time.NewTicker(pollingPeriod)
	for {
		select {
		case <-tk.C:
			offset, err := c.state.LoadOffset()
			if err != nil {
				panic(err)
			}

			messages, err := c.storage.GetMessages(offset)
			if err != nil {
				return fmt.Errorf("failed to GetMessages: %w", err)
			}

			for _, message := range messages {
				if message.RecipientAddr == "" || message.RecipientAddr == c.GetAddr() {
					c.logger.Log("Handling message with offset %d, type %s", message.Offset, message.Event)
					if err := c.ProcessMessage(message); err != nil {
						c.logger.Log("Failed to process message: %v", err)
					} else {
						c.logger.Log("Successfully processed message with offset %d, type %s",
							message.Offset, message.Event)
					}
				}
			}

			operations, err := c.GetOperations()
			if err != nil {
				c.logger.Log("Failed to get operations: %v", err)
			}

			c.logger.Log("Got %d Operations from pool", len(operations))
			for _, operation := range operations {
				c.logger.Log("Handling operation %s in airgapped", operation.Type)
				processedOperation, err := c.Airgapped.HandleOperation(*operation)
				if err != nil {
					c.logger.Log("Failed to handle operation: %v", err)
				}

				c.logger.Log("Got %d Processed Operations from Airgapped", len(operations))
				c.logger.Log("Operation %s handled in airgapped, result event is %s",
					operation.Event, processedOperation.Event)
				if err = c.handleProcessedOperation(processedOperation); err != nil {
					c.logger.Log("Failed to handle processed operation: %v", err)
				} else {
					c.logger.Log("Successfully handled processed operation %s", processedOperation.Event)
				}
			}
		case <-c.ctx.Done():
			log.Println("Context closed, stop polling...")
			return nil
		}
	}
}

func (c *Client) SendMessage(message storage.Message) error {
	if _, err := c.storage.Send(message); err != nil {
		return fmt.Errorf("failed to post message: %w", err)
	}

	return nil
}

func (c *Client) ProcessMessage(message storage.Message) error {
	fsmInstance, err := c.getFSMInstance(message.DkgRoundID)
	if err != nil {
		return fmt.Errorf("failed to getFSMInstance: %w", err)
	}

	if fsm.Event(message.Event) != signature_proposal_fsm.EventInitProposal {
		if err := c.verifyMessage(fsmInstance, message); err != nil {
			return fmt.Errorf("failed to verifyMessage %+v: %w", message, err)
		}
	}

	fsmReq, err := types.FSMRequestFromMessage(message)
	if err != nil {
		return fmt.Errorf("failed to get FSMRequestFromMessage: %v", err)
	}

	resp, fsmDump, err := fsmInstance.Do(fsm.Event(message.Event), fsmReq)
	if err != nil {
		return fmt.Errorf("failed to Do operation in FSM: %w", err)
	}

	c.logger.Log("message %s done successfully from %s", message.Event, message.SenderAddr)

	if resp.State == spf.StateSignatureProposalCollected {
		fsmInstance, err = state_machines.FromDump(fsmDump)
		if err != nil {
			return fmt.Errorf("failed get state_machines from dump: %w", err)
		}
		resp, fsmDump, err = fsmInstance.Do(dpf.EventDKGInitProcess, requests.DefaultRequest{
			CreatedAt: time.Now(),
		})
		if err != nil {
			return fmt.Errorf("failed to Do operation in FSM: %w", err)
		}
	}
	if resp.State == dpf.StateDkgMasterKeyCollected {
		fsmInstance, err = state_machines.FromDump(fsmDump)
		if err != nil {
			return fmt.Errorf("failed get state_machines from dump: %w", err)
		}
		resp, fsmDump, err = fsmInstance.Do(sipf.EventSigningInit, requests.DefaultRequest{
			CreatedAt: time.Now(),
		})
		if err != nil {
			return fmt.Errorf("failed to Do operation in FSM: %w", err)
		}
	}

	var operation *types.Operation
	switch resp.State {
	// if the new state is waiting for RPC to airgapped machine
	case
		spf.StateAwaitParticipantsConfirmations,
		dpf.StateDkgCommitsAwaitConfirmations,
		dpf.StateDkgDealsAwaitConfirmations,
		dpf.StateDkgResponsesAwaitConfirmations,
		dpf.StateDkgMasterKeyAwaitConfirmations,
		sipf.StateSigningAwaitPartialSigns,
		sipf.StateSigningPartialSignsCollected,
		sipf.StateSigningAwaitConfirmations:
		if resp.Data != nil {
			bz, err := json.Marshal(resp.Data)
			if err != nil {
				return fmt.Errorf("failed to marshal FSM response: %w", err)
			}

			operation = &types.Operation{
				ID:            uuid.New().String(),
				Type:          types.OperationType(resp.State),
				Payload:       bz,
				DKGIdentifier: message.DkgRoundID,
				CreatedAt:     time.Now(),
			}
		}
	default:
		c.logger.Log("State %s does not require an operation", resp.State)
	}

	if operation != nil {
		if err := c.state.PutOperation(operation); err != nil {
			return fmt.Errorf("failed to PutOperation: %w", err)
		}
	}

	if err := c.state.SaveOffset(message.Offset + 1); err != nil {
		return fmt.Errorf("failed to SaveOffset: %w", err)
	}

	if err := c.state.SaveFSM(message.DkgRoundID, fsmDump); err != nil {
		return fmt.Errorf("failed to SaveFSM: %w", err)
	}

	return nil
}

func (c *Client) GetOperations() (map[string]*types.Operation, error) {
	return c.state.GetOperations()
}

func (c *Client) getOperationJSON(operationID string) ([]byte, error) {
	operation, err := c.state.GetOperationByID(operationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get operation: %w", err)
	}

	operationJSON, err := json.Marshal(operation)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal operation: %w", err)
	}
	return operationJSON, nil
}

// GetOperationQRPath returns a path to the image with the QR generated
// for the specified operation. It is supposed that the user will open
// this file herself.
func (c *Client) GetOperationQRPath(operationID string) ([]string, error) {
	operationJSON, err := c.getOperationJSON(operationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get operation in JSON: %w", err)
	}

	operationQRPath := filepath.Join(QrCodesDir, operationID)
	chunks, err := qr.DataToChunks(operationJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to divide a data on chunks: %w", err)
	}

	qrs := make([]string, 0, len(chunks))

	for idx, chunk := range chunks {
		qrPath := fmt.Sprintf("%s-%d", operationQRPath, idx)
		if err = c.qrProcessor.WriteQR(qrPath, chunk); err != nil {
			return nil, err
		}
		qrs = append(qrs, qrPath)
	}

	return qrs, nil
}

// ReadProcessedOperation reads the processed operation from camera, checks that
// the processed operation has its unprocessed counterpart in our state,
// posts a Message to the storage and deletes the operation from our state.
func (c *Client) ReadProcessedOperation() error {
	bz, err := qr.ReadDataFromQRChunks(c.qrProcessor)
	if err != nil {
		return fmt.Errorf("failed to ReadQR: %s", err)
	}

	var operation types.Operation
	if err = json.Unmarshal(bz, &operation); err != nil {
		return fmt.Errorf("failed to unmarshal processed operation")
	}

	return c.handleProcessedOperation(operation)
}

func (c *Client) handleProcessedOperation(operation types.Operation) error {
	storedOperation, err := c.state.GetOperationByID(operation.ID)
	if err != nil {
		return fmt.Errorf("failed to find matching operation: %w", err)
	}

	if err := storedOperation.Check(&operation); err != nil {
		return fmt.Errorf("processed operation does not match stored operation: %w", err)
	}

	for _, message := range operation.ResultMsgs {
		message.SenderAddr = c.GetAddr()

		sig, err := c.signMessage(message.Bytes())
		if err != nil {
			return fmt.Errorf("failed to sign a message: %w", err)
		}
		message.Signature = sig

		if _, err := c.storage.Send(message); err != nil {
			return fmt.Errorf("failed to post message: %w", err)
		}
	}

	if err := c.state.DeleteOperation(operation.ID); err != nil {
		return fmt.Errorf("failed to DeleteOperation: %w", err)
	}

	return nil
}

func (c *Client) getFSMInstance(dkgRoundID string) (*state_machines.FSMInstance, error) {
	var err error
	fsmInstance, ok, err := c.state.LoadFSM(dkgRoundID)
	if err != nil {
		return nil, fmt.Errorf("failed to LoadFSM: %w", err)
	}

	if !ok {
		fsmInstance, err = state_machines.Create(dkgRoundID)
		if err != nil {
			return nil, fmt.Errorf("failed to create FSM instance: %w", err)
		}
		bz, err := fsmInstance.Dump()
		if err != nil {
			return nil, fmt.Errorf("failed to Dump FSM instance: %w", err)
		}
		if err := c.state.SaveFSM(dkgRoundID, bz); err != nil {
			return nil, fmt.Errorf("failed to SaveFSM: %w", err)
		}
	}

	return fsmInstance, nil
}

func (c *Client) signMessage(message []byte) ([]byte, error) {
	keyPair, err := c.keyStore.LoadKeys(c.userName, "")
	if err != nil {
		return nil, fmt.Errorf("failed to LoadKeys: %w", err)
	}

	return ed25519.Sign(keyPair.Priv, message), nil
}

func (c *Client) verifyMessage(fsmInstance *state_machines.FSMInstance, message storage.Message) error {
	senderPubKey, err := fsmInstance.GetPubKeyByAddr(message.SenderAddr)
	if err != nil {
		return fmt.Errorf("failed to GetPubKeyByAddr: %w", err)
	}

	if !ed25519.Verify(senderPubKey, message.Bytes(), message.Signature) {
		return errors.New("signature is corrupt")
	}

	return nil
}
