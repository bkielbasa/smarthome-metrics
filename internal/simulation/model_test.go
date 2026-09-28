package simulation

import (
	"math"
	"testing"
)

func TestBatteryModel_SurplusCharging(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(0.0)

	// Solar = 6000W, Load = 1000W -> Surplus = 5000W
	// Duration = 1 hour (1.0h)
	out := model.Step(StepInput{
		LoadPowerW:    1000,
		PVPowerW:      6000,
		RCEKWh:        0.50,
		DurationHours: 1.0,
	})

	// Battery power should be clamped to MaxPowerKW = 5000W
	if out.BatteryPowerW != 5000 {
		t.Errorf("expected BatteryPowerW = 5000, got %v", out.BatteryPowerW)
	}
	// With 0.95 efficiency: 5 kWh * 0.95 = 4.75 kWh stored
	expectedEnergy := 5.0 * 0.95
	if out.BatteryEnergyKWh != expectedEnergy {
		t.Errorf("expected BatteryEnergyKWh = %v, got %v", expectedEnergy, out.BatteryEnergyKWh)
	}
}

func TestBatteryModel_DeficitDischarging(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(5.0)

	// Solar = 0W, Load = 2000W -> Deficit = 2000W
	// Duration = 1 hour (1.0h)
	out := model.Step(StepInput{
		LoadPowerW:    2000,
		PVPowerW:      0,
		RCEKWh:        0.60,
		DurationHours: 1.0,
	})

	// Battery discharges to supply 2000W to load
	if out.BatteryPowerW != -2000 {
		t.Errorf("expected BatteryPowerW = -2000, got %v", out.BatteryPowerW)
	}
	// Net grid power should be 0 (all supplied by battery)
	if out.NetGridPowerW != 0 {
		t.Errorf("expected NetGridPowerW = 0, got %v", out.NetGridPowerW)
	}
	// Savings: avoided buying 2 kWh at (0.60 + 0.40) = 2.00 PLN
	if out.SavingsIntervalPLN <= 0 {
		t.Errorf("expected positive savings, got %v", out.SavingsIntervalPLN)
	}
}

func TestBatteryModel_CapacityClamping(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 1.0, 0.40)
	model.SetEnergy(9.0)

	// Surplus 5000W for 1h would add 5 kWh -> capped at 10.0 kWh
	out := model.Step(StepInput{
		LoadPowerW:    0,
		PVPowerW:      5000,
		RCEKWh:        0.50,
		DurationHours: 1.0,
	})
	if out.BatteryEnergyKWh != 10.0 {
		t.Errorf("expected energy capped at 10.0, got %v", out.BatteryEnergyKWh)
	}
}

func TestBatteryModel_LowPriceArbitrage(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(2.0)

	// Night time: Solar = 0, Load = 500W, RCE is very low = 0.05 PLN/kWh
	out := model.Step(StepInput{
		LoadPowerW:    500,
		PVPowerW:      0,
		RCEKWh:        0.05,
		DurationHours: 1.0,
	})

	// When price is ultra low (< 0.15), battery charges from grid rather than discharging
	if out.BatteryPowerW <= 0 {
		t.Errorf("expected battery to charge from cheap grid, got power %v", out.BatteryPowerW)
	}
}

func TestBatteryModel_DeficitDischarging_EmptyBattery(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(0.0)

	// Empty battery cannot discharge
	out := model.Step(StepInput{
		LoadPowerW:    2000,
		PVPowerW:      0,
		RCEKWh:        0.60,
		DurationHours: 1.0,
	})

	if out.BatteryPowerW != 0 {
		t.Errorf("expected BatteryPowerW = 0 for empty battery, got %v", out.BatteryPowerW)
	}
	if out.BatteryEnergyKWh != 0 {
		t.Errorf("expected BatteryEnergyKWh = 0, got %v", out.BatteryEnergyKWh)
	}
	if out.NetGridPowerW != -2000 {
		t.Errorf("expected NetGridPowerW = -2000, got %v", out.NetGridPowerW)
	}
	if out.SavingsIntervalPLN != 0 {
		t.Errorf("expected SavingsIntervalPLN = 0, got %v", out.SavingsIntervalPLN)
	}
}

func TestBatteryModel_HighPriceArbitrage(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 1.0, 0.40)
	model.SetEnergy(10.0) // 100% > 50%, 5 kWh available above 50% floor

	// High price peak: RCE = 0.95 > 0.85 PLN/kWh
	// Load = 1000W, PV = 0W, MaxPower = 5kW
	// Battery should cover 1000W load AND export 4000W to grid
	out := model.Step(StepInput{
		LoadPowerW:    1000,
		PVPowerW:      0,
		RCEKWh:        0.95,
		DurationHours: 1.0,
	})

	if out.BatteryPowerW != -5000 {
		t.Errorf("expected BatteryPowerW = -5000, got %v", out.BatteryPowerW)
	}
	if out.NetGridPowerW != 4000 {
		t.Errorf("expected NetGridPowerW = 4000, got %v", out.NetGridPowerW)
	}
	// Baseline: import 1 kWh at (0.95 + 0.40) = 1.35 PLN cost -> cashflow = -1.35 PLN
	// Simulated: export 4 kWh at 0.95 = +3.80 PLN revenue -> cashflow = +3.80 PLN
	// Savings = 3.80 - (-1.35) = 5.15 PLN
	expectedSavings := 3.80 - (-1.35)
	if math.Abs(out.SavingsIntervalPLN-expectedSavings) > 1e-6 {
		t.Errorf("expected savings %v, got %v", expectedSavings, out.SavingsIntervalPLN)
	}
}

func TestBatteryModel_RoundTripEfficiency(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(0.0)

	// Step 1: Charge 5 kWh (at 5000W for 1h)
	outCharge := model.Step(StepInput{
		LoadPowerW:    0,
		PVPowerW:      5000,
		RCEKWh:        0.50,
		DurationHours: 1.0,
	})
	// 5.0 * 0.95 = 4.75 kWh stored
	if math.Abs(outCharge.BatteryEnergyKWh-4.75) > 1e-6 {
		t.Fatalf("expected 4.75 kWh, got %v", outCharge.BatteryEnergyKWh)
	}

	// Step 2: Discharge all stored energy
	// Max discharge power delivering all stored energy: 4.75 * 0.95 = 4.5125 kWh in 1h = 4512.5 W
	outDischarge := model.Step(StepInput{
		LoadPowerW:    5000,
		PVPowerW:      0,
		RCEKWh:        0.50,
		DurationHours: 1.0,
	})
	expectedDischargePowerW := -(4.75 * 0.95 * 1000.0)
	if math.Abs(outDischarge.BatteryPowerW-expectedDischargePowerW) > 1e-6 {
		t.Errorf("expected BatteryPowerW = %v, got %v", expectedDischargePowerW, outDischarge.BatteryPowerW)
	}
	if math.Abs(outDischarge.BatteryEnergyKWh) > 1e-6 {
		t.Errorf("expected BatteryEnergyKWh = 0, got %v", outDischarge.BatteryEnergyKWh)
	}
	// Delivered energy / input energy = 4.5125 / 5.0 = 0.9025 (0.95 * 0.95)
	roundTripRatio := (-outDischarge.BatteryPowerW / 1000.0) / (outCharge.BatteryPowerW / 1000.0)
	if math.Abs(roundTripRatio-0.9025) > 1e-6 {
		t.Errorf("expected round trip ratio 0.9025, got %v", roundTripRatio)
	}
}

func TestBatteryModel_SetEnergyClamping(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)

	model.SetEnergy(-5.0)
	if model.Energy() != 0.0 {
		t.Errorf("expected 0.0, got %v", model.Energy())
	}

	model.SetEnergy(15.0)
	if model.Energy() != 10.0 {
		t.Errorf("expected 10.0, got %v", model.Energy())
	}

	model.SetEnergy(6.5)
	if model.Energy() != 6.5 {
		t.Errorf("expected 6.5, got %v", model.Energy())
	}
}

func TestBatteryModel_ZeroDuration(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(5.0)

	out := model.Step(StepInput{
		LoadPowerW:    2000,
		PVPowerW:      5000,
		RCEKWh:        0.50,
		DurationHours: 0.0,
	})

	if out.BatteryEnergyKWh != 5.0 {
		t.Errorf("expected 5.0, got %v", out.BatteryEnergyKWh)
	}
	if out.BatteryPowerW != 0 {
		t.Errorf("expected 0, got %v", out.BatteryPowerW)
	}
	if out.SavingsIntervalPLN != 0 {
		t.Errorf("expected 0, got %v", out.SavingsIntervalPLN)
	}
}
