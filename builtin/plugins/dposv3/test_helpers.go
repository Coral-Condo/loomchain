package dposv3

import (
	"math/big"

	loom "github.com/loomnetwork/go-loom"
	common "github.com/loomnetwork/go-loom/common"
	"github.com/loomnetwork/go-loom/plugin"
	contract "github.com/loomnetwork/go-loom/plugin/contractpb"
	"github.com/loomnetwork/go-loom/types"
	// "github.com/loomnetwork/loomchain"
	// "github.com/loomnetwork/loomchain/builtin/plugins/coin"
)

type TestDPOSContract struct {
	Contract *DPOS
	Address  loom.Address
}

func deployDPOSContract(
	ctx *plugin.FakeContext,
	params *Params,
) (*TestDPOSContract, error) {
	dposContract := &DPOS{}
	contractAddr := ctx.CreateContract(contract.MakePluginContract(dposContract))
	contractCtx := contract.WrapPluginContext(ctx.WithAddress(contractAddr))

	err := dposContract.Init(contractCtx, &InitRequest{
		Params: params,
		// may also want to set validators
	})

	return &TestDPOSContract{
		Contract: dposContract,
		Address:  contractAddr,
	}, err
}

func (dpos *TestDPOSContract) ListAllDelegations(ctx *plugin.FakeContext) ([]*ListDelegationsResponse, error) {
	resp, err := dpos.Contract.ListAllDelegations(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&ListAllDelegationsRequest{},
	)
	if err != nil {
		return nil, err
	}

	return resp.ListResponses, err
}

func (dpos *TestDPOSContract) ListCandidates(ctx *plugin.FakeContext) ([]*CandidateStatistic, error) {
	resp, err := dpos.Contract.ListCandidates(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&ListCandidatesRequest{},
	)
	if err != nil {
		return nil, err
	}
	return resp.Candidates, err
}

func (dpos *TestDPOSContract) ListValidators(ctx *plugin.FakeContext) ([]*ValidatorStatistic, error) {
	resp, err := dpos.Contract.ListValidators(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&ListValidatorsRequest{},
	)
	if err != nil {
		return nil, err
	}
	return resp.Statistics, err
}

func (dpos *TestDPOSContract) CheckRewards(ctx *plugin.FakeContext) (*common.BigUInt, error) {
	resp, err := dpos.Contract.CheckRewards(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&CheckRewardsRequest{},
	)
	if err != nil {
		return nil, err
	}
	return &resp.TotalRewardDistribution.Value, err
}

func (dpos *TestDPOSContract) CheckRewardDelegation(ctx *plugin.FakeContext, validator *loom.Address) (*Delegation, error) {
	resp, err := dpos.Contract.CheckRewardDelegation(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&CheckRewardDelegationRequest{
			ValidatorAddress: validator.MarshalPB(),
		},
	)
	if err != nil {
		return nil, err
	}
	return resp.Delegation, nil
}

func (dpos *TestDPOSContract) CheckDelegation(ctx *plugin.FakeContext, validator *loom.Address, delegator *loom.Address) ([]*Delegation, *big.Int, *big.Int, error) {
	resp, err := dpos.Contract.CheckDelegation(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&CheckDelegationRequest{
			ValidatorAddress: validator.MarshalPB(),
			DelegatorAddress: delegator.MarshalPB(),
		},
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return resp.Delegations, resp.Amount.Value.Int, resp.WeightedAmount.Value.Int, nil
}

func (dpos *TestDPOSContract) CheckAllDelegations(ctx *plugin.FakeContext, delegator *loom.Address) ([]*Delegation, *big.Int, *big.Int, error) {
	resp, err := dpos.Contract.CheckAllDelegations(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&CheckAllDelegationsRequest{
			DelegatorAddress: delegator.MarshalPB(),
		},
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return resp.Delegations, resp.Amount.Value.Int, resp.WeightedAmount.Value.Int, nil
}

func (dpos *TestDPOSContract) RegisterReferrer(ctx *plugin.FakeContext, referrer loom.Address, name string) error {
	err := dpos.Contract.RegisterReferrer(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&RegisterReferrerRequest{
			Name:    name,
			Address: referrer.MarshalPB(),
		},
	)
	return err
}

func (dpos *TestDPOSContract) WhitelistCandidate(ctx *plugin.FakeContext, candidate loom.Address, amount *big.Int, tier LocktimeTier) error {
	err := dpos.Contract.WhitelistCandidate(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&WhitelistCandidateRequest{
			CandidateAddress: candidate.MarshalPB(),
			Amount:           &types.BigUInt{Value: *loom.NewBigUInt(amount)},
			LocktimeTier:     tier,
		},
	)
	return err
}

func (dpos *TestDPOSContract) ChangeWhitelistInfo(ctx *plugin.FakeContext, candidate *loom.Address, amount *big.Int, tier *LocktimeTier) error {
	req := &ChangeWhitelistInfoRequest{
		CandidateAddress: candidate.MarshalPB(),
		Amount:           &types.BigUInt{Value: *loom.NewBigUInt(amount)},
	}
	if tier != nil {
		req.LocktimeTier = *tier
	}
	err := dpos.Contract.ChangeWhitelistInfo(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		req,
	)
	return err
}

func (dpos *TestDPOSContract) ChangeFee(ctx *plugin.FakeContext, candidateFee uint64) error {
	err := dpos.Contract.ChangeFee(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&ChangeCandidateFeeRequest{
			Fee: candidateFee,
		},
	)
	return err
}

func (dpos *TestDPOSContract) RegisterCandidate(
	ctx *plugin.FakeContext,
	pubKey []byte,
	tier *uint64,
	candidateFee *uint64,
	maxReferralPercentage *uint64,
	candidateName *string,
	candidateDescription *string,
	candidateWebsite *string,
) error {
	req := RegisterCandidateRequest{
		PubKey: pubKey,
	}

	if maxReferralPercentage != nil {
		req.MaxReferralPercentage = *maxReferralPercentage
	}

	if tier != nil {
		req.LocktimeTier = *tier
	}

	if candidateFee != nil {
		req.Fee = *candidateFee
	}

	if candidateName != nil {
		req.Name = *candidateName
	}

	if candidateDescription != nil {
		req.Description = *candidateDescription
	}

	if candidateWebsite != nil {
		req.Website = *candidateWebsite
	}

	err := dpos.Contract.RegisterCandidate(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&req,
	)
	return err
}

func (dpos *TestDPOSContract) UnregisterCandidate(ctx *plugin.FakeContext) error {
	err := dpos.Contract.UnregisterCandidate(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&UnregisterCandidateRequest{},
	)
	return err
}

func (dpos *TestDPOSContract) RemoveWhitelistedCandidate(ctx *plugin.FakeContext, candidate *loom.Address) error {
	err := dpos.Contract.RemoveWhitelistedCandidate(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&RemoveWhitelistedCandidateRequest{CandidateAddress: candidate.MarshalPB()},
	)
	return err
}

func (dpos *TestDPOSContract) Delegate(ctx *plugin.FakeContext, validator *loom.Address, amount *big.Int, tier *uint64, referrer *string) error {
	req := &DelegateRequest{
		ValidatorAddress: validator.MarshalPB(),
		Amount:           &types.BigUInt{Value: *loom.NewBigUInt(amount)},
	}
	if tier != nil {
		req.LocktimeTier = *tier
	}

	if referrer != nil {
		req.Referrer = *referrer
	}

	err := dpos.Contract.Delegate(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		req,
	)
	return err
}

func (dpos *TestDPOSContract) Redelegate(ctx *plugin.FakeContext, validator *loom.Address, newValidator *loom.Address, amount *big.Int, index uint64, tier *uint64, referrer *string) error {
	req := &RedelegateRequest{
		FormerValidatorAddress: validator.MarshalPB(),
		ValidatorAddress:       newValidator.MarshalPB(),
		Index:                  index,
	}

	if amount != nil {
		req.Amount = &types.BigUInt{Value: *loom.NewBigUInt(amount)}
	}

	if tier != nil {
		req.NewLocktimeTier = *tier
	}

	if referrer != nil {
		req.Referrer = *referrer
	}

	err := dpos.Contract.Redelegate(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		req,
	)
	return err
}

func (dpos *TestDPOSContract) Unbond(ctx *plugin.FakeContext, validator *loom.Address, amount *big.Int, index uint64) error {
	err := dpos.Contract.Unbond(
		contract.WrapPluginContext(ctx.WithAddress(dpos.Address)),
		&UnbondRequest{
			ValidatorAddress: validator.MarshalPB(),
			Amount:           &types.BigUInt{Value: *loom.NewBigUInt(amount)},
			Index:            index,
		},
	)
	return err
}
