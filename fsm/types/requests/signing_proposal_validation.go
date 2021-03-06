package requests

import "errors"

func (r *SigningProposalStartRequest) Validate() error {
	if r.ParticipantId < 0 {
		return errors.New("{ParticipantId} cannot be a negative number")
	}

	if len(r.SrcPayload) == 0 {
		return errors.New("{SrcPayload} cannot zero length")
	}

	if r.CreatedAt.IsZero() {
		return errors.New("{CreatedAt} is not set")
	}

	return nil
}

func (r *SigningProposalParticipantRequest) Validate() error {
	if r.SigningId == "" {
		return errors.New("{SigningId} cannot be empty")
	}

	if r.ParticipantId < 0 {
		return errors.New("{ParticipantId} cannot be a negative number")
	}

	if r.CreatedAt.IsZero() {
		return errors.New("{CreatedAt} is not set")
	}

	return nil
}

func (r *SigningProposalPartialSignRequest) Validate() error {
	if r.SigningId == "" {
		return errors.New("{SigningId} cannot be empty")
	}

	if r.ParticipantId < 0 {
		return errors.New("{ParticipantId} cannot be a negative number")
	}

	if len(r.PartialSign) == 0 {
		return errors.New("{PartialSign} cannot zero length")
	}

	if r.CreatedAt.IsZero() {
		return errors.New("{CreatedAt} is not set")
	}

	return nil
}
