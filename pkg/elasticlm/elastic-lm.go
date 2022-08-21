package elasticlm

import (
	"context"
	"strconv"
	"time"

	"github.com/hiepnv90/elastic-lm/pkg/common"
	"github.com/hiepnv90/elastic-lm/pkg/graphql"
	"github.com/hiepnv90/elastic-lm/pkg/position"
	"go.uber.org/zap"
)

type ElasticLM struct {
	interval    time.Duration
	positionIDs []string
	positionMap map[string]position.Position

	client *graphql.Client
	logger *zap.SugaredLogger
}

func New(client *graphql.Client, positionIDs []string, interval time.Duration) *ElasticLM {
	return &ElasticLM{
		interval:    interval,
		positionIDs: positionIDs,
		positionMap: make(map[string]position.Position),
		client:      client,
		logger:      zap.S(),
	}
}

func (e *ElasticLM) Start(ctx context.Context) error {
	l := e.logger.With("positions", e.positionIDs, "interval", e.interval)

	l.Infow("Start monitoring positions")

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	err := e.updatePositions(ctx)
	if err != nil {
		l.Errorw("Fail to update positions' information", "error", err)
		return err
	}

	for {
		select {
		case <-ctx.Done():
			l.Infow("Stop monitoring positions")
			return nil
		case <-ticker.C:
			err = e.updatePositions(ctx)
			if err != nil {
				l.Errorw("Fail to update positions' information", "error", err)
			}
		}
	}
}

func (e *ElasticLM) updatePositions(ctx context.Context) error {
	l := e.logger

	posInfos, err := e.getPositions(ctx)
	if err != nil {
		l.Errorw("Fail to get positions' information", "positions", e.positionIDs, "error", err)
		return err
	}

	for _, posInfo := range posInfos {
		oldPosInfo, ok := e.positionMap[posInfo.ID]
		if !ok || !oldPosInfo.Equal(posInfo) {
			l.Infow("Update position's information", "info", posInfo.String())
			e.positionMap[posInfo.ID] = posInfo
		}
	}

	return nil
}

func (e *ElasticLM) getPositions(ctx context.Context) ([]position.Position, error) {
	l := e.logger

	l.Debugw("Get positions' information", "positions", e.positionIDs)

	positions, err := e.client.GetPositions(e.positionIDs)
	if err != nil {
		l.Errorw("Fail to get position liquidity", "positions", e.positionIDs, "error", err)
		return nil, err
	}

	res := make([]position.Position, 0, len(positions))
	for _, posData := range positions {
		l.Debugw("Position information", "posInfo", posData)

		currentTick, err := strconv.Atoi(posData.Pool.Tick)
		if err != nil {
			l.Errorw("Fail to parse current tick", "tick", posData.Pool.Tick, "error", err)
			return nil, err
		}

		tickLower, err := strconv.Atoi(posData.TickLower.TickIdx)
		if err != nil {
			l.Errorw("Fail to parse tick lower", "tick", posData.TickLower.TickIdx, "error", err)
			return nil, err
		}

		tickUpper, err := strconv.Atoi(posData.TickUpper.TickIdx)
		if err != nil {
			l.Errorw("Fail to parse tick upper", "tick", posData.TickUpper.TickIdx, "error", err)
			return nil, err
		}

		sqrtPrice := common.NewBigIntFromString(posData.Pool.SqrtPrice, 10)
		liquidity := common.NewBigIntFromString(posData.Liquidity, 10)

		token0Decimals, err := strconv.Atoi(posData.Pool.Token0.Decimals)
		if err != nil {
			l.Errorw("Fail to parse token0 decimals", "decimals", posData.Pool.Token0.Decimals, "error", err)
			return nil, err
		}

		token1Decimals, err := strconv.Atoi(posData.Pool.Token1.Decimals)
		if err != nil {
			l.Errorw("Fail to parse token1 decimals", "decimals", posData.Pool.Token1.Decimals, "error", err)
			return nil, err
		}

		amount0, amount1 := common.ExtractLiquidity(currentTick, tickLower, tickUpper, sqrtPrice, liquidity)
		res = append(res, position.Position{
			ID: posData.ID,
			Token0: common.Token{
				Amount:   amount0,
				Symbol:   posData.Pool.Token0.Symbol,
				Decimals: token0Decimals,
			},
			Token1: common.Token{
				Amount:   amount1,
				Symbol:   posData.Pool.Token1.Symbol,
				Decimals: token1Decimals,
			},
		})
	}

	return res, nil
}