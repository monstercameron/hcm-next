package performance

// DefaultHardLimits is the safety ceiling shared by the published planning
// fixtures. A deployment may choose lower limits, never higher ones.
var DefaultHardLimits = HardLimits{
	Tenants: 1000, ConcurrentUsers: 10000, CommandsPerSecond: 5000, Fanout: 100,
	PayloadBytes: 4 << 20, Objects: 10000, Integrations: 100, AgentRuns: 1000,
	PeakMultiplier: 4,
	Resources:      ResourceBudget{CPUMillicores: 16000, MemoryMiB: 32768, DBConnections: 500, QueueDepth: 100000, StorageMBps: 1000},
	Correctness:    CorrectnessBudget{ErrorRatePPM: 1000, DuplicateEffects: 0, LostTransactions: 0, MaxRetryAmplification: 3},
}

func fixture(id string, tier Tier, tenants, users, cps, fanout, payload, objects, integrations, agents int, peak float64, resources ResourceBudget) Envelope {
	return Envelope{SchemaVersion: SchemaVersion, ID: id, Version: 1, Tier: tier, Tenants: tenants, ConcurrentUsers: users, CommandsPerSecond: cps, Fanout: fanout, PayloadBytes: payload, Objects: objects, Integrations: integrations, AgentRuns: agents, PeakMultiplier: peak, Degradation: DegradeQueue, Resources: resources, Correctness: CorrectnessBudget{ErrorRatePPM: 1000, MaxRetryAmplification: 3}}
}

var Small = fixture("workload-small", TierSmall, 10, 100, 20, 10, 64<<10, 100, 5, 20, 1.5, ResourceBudget{500, 512, 20, 1000, 10})
var Medium = fixture("workload-medium", TierMedium, 100, 1000, 200, 25, 256<<10, 1000, 20, 100, 2, ResourceBudget{2000, 4096, 80, 10000, 100})
var Large = fixture("workload-large", TierLarge, 500, 5000, 1000, 50, 1<<20, 5000, 50, 500, 3, ResourceBudget{8000, 16384, 250, 50000, 500})
var Peak = fixture("workload-peak", TierPeak, 1000, 10000, 5000, 100, 4<<20, 10000, 100, 1000, 4, ResourceBudget{16000, 32768, 500, 100000, 1000})

func Fixtures() []Envelope { return []Envelope{Small, Medium, Large, Peak} }
