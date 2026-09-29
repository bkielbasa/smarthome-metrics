package simulation

import (
	"sync"
)

// BatteryModel simulates physical battery state and calculates financial delta.
type BatteryModel struct {
	mu               sync.Mutex
	CapacityKWh      float64
	MaxPowerKW       float64
	Efficiency       float64
	DistributionFee  float64
	currentEnergyKWh float64
}

// NewBatteryModel constructs a new BatteryModel with the given parameters.
func NewBatteryModel(capacityKWh, maxPowerKW, efficiency, distributionFee float64) *BatteryModel {
	if capacityKWh < 0 {
		capacityKWh = 0
	}
	if maxPowerKW < 0 {
		maxPowerKW = 0
	}
	if efficiency <= 0 || efficiency > 1.0 {
		efficiency = 0.95
	}
	if distributionFee < 0 {
		distributionFee = 0
	}

	return &BatteryModel{
		CapacityKWh:      capacityKWh,
		MaxPowerKW:       maxPowerKW,
		Efficiency:       efficiency,
		DistributionFee:  distributionFee,
		currentEnergyKWh: capacityKWh * 0.5,
	}
}

// SetEnergy sets the stored energy in kWh, clamped between 0 and CapacityKWh.
func (m *BatteryModel) SetEnergy(kwh float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if kwh < 0 {
		kwh = 0
	}
	if kwh > m.CapacityKWh {
		kwh = m.CapacityKWh
	}
	m.currentEnergyKWh = kwh
}

// Energy returns the current stored energy in kWh.
func (m *BatteryModel) Energy() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentEnergyKWh
}

// Capacity returns the total capacity in kWh.
func (m *BatteryModel) Capacity() float64 {
	return m.CapacityKWh
}

// StepInput defines the physical and economic inputs for a single simulation interval.
type StepInput struct {
	LoadPowerW    float64
	PVPowerW      float64
	RCEKWh        float64
	DurationHours float64
}

// StepOutput defines the simulation output metrics for a single interval.
type StepOutput struct {
	BatteryEnergyKWh   float64
	BatteryPowerW      float64
	BatterySoCPct      float64
	SavingsIntervalPLN float64
	NetGridPowerW      float64
}

// Step advances the battery model by DurationHours given the step inputs and dispatches
// power according to the Smart Hybrid arbitrage strategy.
func (m *BatteryModel) Step(in StepInput) StepOutput {
	m.mu.Lock()
	defer m.mu.Unlock()

	socPct := 0.0
	if m.CapacityKWh > 0 {
		socPct = (m.currentEnergyKWh / m.CapacityKWh) * 100.0
	}

	if in.DurationHours <= 0 {
		return StepOutput{
			BatteryEnergyKWh:   m.currentEnergyKWh,
			BatteryPowerW:      0,
			BatterySoCPct:      socPct,
			SavingsIntervalPLN: 0,
			NetGridPowerW:      in.PVPowerW - in.LoadPowerW,
		}
	}

	eff := m.Efficiency
	if eff <= 0 || eff > 1.0 {
		eff = 0.95
	}
	maxPowerW := m.MaxPowerKW * 1000.0
	if maxPowerW < 0 {
		maxPowerW = 0
	}

	// Maximum charging power allowed by available capacity headroom
	headroomKWh := m.CapacityKWh - m.currentEnergyKWh
	if headroomKWh < 0 {
		headroomKWh = 0
	}
	maxChargePowerFromHeadroomW := (headroomKWh / eff / in.DurationHours) * 1000.0
	allowedChargePowerW := maxPowerW
	if maxChargePowerFromHeadroomW < allowedChargePowerW {
		allowedChargePowerW = maxChargePowerFromHeadroomW
	}

	// Maximum discharging power allowed by available energy
	availEnergyKWh := m.currentEnergyKWh
	if availEnergyKWh < 0 {
		availEnergyKWh = 0
	}
	maxDischargePowerFromEnergyW := (availEnergyKWh * eff / in.DurationHours) * 1000.0
	allowedDischargePowerW := maxPowerW
	if maxDischargePowerFromEnergyW < allowedDischargePowerW {
		allowedDischargePowerW = maxDischargePowerFromEnergyW
	}

	var batteryPowerW float64

	// Smart Hybrid Strategy
	// 1. Low Price Arbitrage: SoC < 80% and RCE < 0.15 PLN/kWh
	if socPct < 80.0 && in.RCEKWh < 0.15 {
		target80KWh := 0.80 * m.CapacityKWh
		headroom80KWh := target80KWh - m.currentEnergyKWh
		if headroom80KWh < 0 {
			headroom80KWh = 0
		}
		maxGridChargePowerW := (headroom80KWh / eff / in.DurationHours) * 1000.0
		if maxGridChargePowerW > maxPowerW {
			maxGridChargePowerW = maxPowerW
		}

		surplus := in.PVPowerW - in.LoadPowerW
		if surplus > 0 {
			if surplus >= allowedChargePowerW {
				batteryPowerW = allowedChargePowerW
			} else {
				totalCharge := surplus + maxGridChargePowerW
				if totalCharge > allowedChargePowerW {
					totalCharge = allowedChargePowerW
				}
				if totalCharge > maxPowerW {
					totalCharge = maxPowerW
				}
				batteryPowerW = totalCharge
			}
		} else {
			batteryPowerW = min(maxGridChargePowerW, allowedChargePowerW)
		}
	} else if socPct > 50.0 && in.RCEKWh > 0.85 {
		// 2. High Price Arbitrage: SoC > 50% and RCE > 0.85 PLN/kWh
		deficit := in.LoadPowerW - in.PVPowerW
		if deficit > 0 {
			coverLoad := deficit
			if coverLoad > allowedDischargePowerW {
				coverLoad = allowedDischargePowerW
			}
			remainingInverterW := maxPowerW - coverLoad

			floor50KWh := 0.50 * m.CapacityKWh
			availAbove50KWh := m.currentEnergyKWh - floor50KWh
			if availAbove50KWh < 0 {
				availAbove50KWh = 0
			}
			energyUsedCoverLoad := (coverLoad * in.DurationHours / 1000.0) / eff
			availAbove50KWh -= energyUsedCoverLoad
			if availAbove50KWh < 0 {
				availAbove50KWh = 0
			}

			maxGridDischargePowerW := (availAbove50KWh * eff / in.DurationHours) * 1000.0
			gridDischarge := remainingInverterW
			if maxGridDischargePowerW < gridDischarge {
				gridDischarge = maxGridDischargePowerW
			}
			batteryPowerW = -(coverLoad + gridDischarge)
		} else {
			floor50KWh := 0.50 * m.CapacityKWh
			availAbove50KWh := m.currentEnergyKWh - floor50KWh
			if availAbove50KWh < 0 {
				availAbove50KWh = 0
			}
			maxGridDischargePowerW := (availAbove50KWh * eff / in.DurationHours) * 1000.0
			gridDischarge := maxPowerW
			if maxGridDischargePowerW < gridDischarge {
				gridDischarge = maxGridDischargePowerW
			}
			batteryPowerW = -gridDischarge
		}
	} else if in.PVPowerW > in.LoadPowerW {
		// 3. Normal Surplus: Solar charges battery up to inverter limit / capacity
		surplus := in.PVPowerW - in.LoadPowerW
		chargePower := surplus
		if chargePower > allowedChargePowerW {
			chargePower = allowedChargePowerW
		}
		batteryPowerW = chargePower
	} else if in.LoadPowerW > in.PVPowerW {
		// 4. Normal Deficit: Battery discharges to cover load up to inverter limit / energy
		deficit := in.LoadPowerW - in.PVPowerW
		dischargePower := deficit
		if dischargePower > allowedDischargePowerW {
			dischargePower = allowedDischargePowerW
		}
		batteryPowerW = -dischargePower
	} else {
		// 5. Balanced
		batteryPowerW = 0
	}

	// Update stored energy
	if batteryPowerW > 0 {
		energyInKWh := (batteryPowerW * in.DurationHours) / 1000.0
		energyAddedKWh := energyInKWh * eff
		m.currentEnergyKWh += energyAddedKWh
		if m.currentEnergyKWh > m.CapacityKWh {
			m.currentEnergyKWh = m.CapacityKWh
		}
	} else if batteryPowerW < 0 {
		energyOutKWh := (-batteryPowerW * in.DurationHours) / 1000.0
		energyDrawnKWh := energyOutKWh / eff
		m.currentEnergyKWh -= energyDrawnKWh
		if m.currentEnergyKWh < 0 {
			m.currentEnergyKWh = 0
		}
	}

	// Net grid power (+ = export to grid, - = import from grid)
	netGridPowerW := in.PVPowerW - in.LoadPowerW - batteryPowerW

	// Financial cashflow calculation
	// 1. Baseline Scenario (without battery)
	pNet := in.PVPowerW - in.LoadPowerW
	var baselineCashflow float64
	if pNet >= 0 {
		exportKWh := (pNet * in.DurationHours) / 1000.0
		baselineRevenue := exportKWh * in.RCEKWh
		baselineCashflow = baselineRevenue
	} else {
		importKWh := (-pNet * in.DurationHours) / 1000.0
		baselineCost := importKWh * (in.RCEKWh + m.DistributionFee)
		baselineCashflow = -baselineCost
	}

	// 2. Simulated Scenario (with battery)
	var simCashflow float64
	if netGridPowerW >= 0 {
		simExportKWh := (netGridPowerW * in.DurationHours) / 1000.0
		simRevenue := simExportKWh * in.RCEKWh
		simCashflow = simRevenue
	} else {
		simImportKWh := (-netGridPowerW * in.DurationHours) / 1000.0
		simCost := simImportKWh * (in.RCEKWh + m.DistributionFee)
		simCashflow = -simCost
	}

	savingsIntervalPLN := simCashflow - baselineCashflow

	newSoCPct := 0.0
	if m.CapacityKWh > 0 {
		newSoCPct = (m.currentEnergyKWh / m.CapacityKWh) * 100.0
	}

	return StepOutput{
		BatteryEnergyKWh:   m.currentEnergyKWh,
		BatteryPowerW:      batteryPowerW,
		BatterySoCPct:      newSoCPct,
		SavingsIntervalPLN: savingsIntervalPLN,
		NetGridPowerW:      netGridPowerW,
	}
}
