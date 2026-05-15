export type CompensationMarketPercentiles = {
  p10: number;
  p25: number;
  p50: number;
  p75: number;
  p90: number;
};

export type CompensationMarketRecord = {
  marketDataId: string;
  provider: string;
  jobCode: string;
  jobTitle: string;
  jobFamily: string;
  level: string;
  location: string;
  payZone: string;
  currency: string;
  annualBaseSalary: CompensationMarketPercentiles;
  annualTotalCash: CompensationMarketPercentiles;
  sampleSize: number;
  confidence: "low" | "medium" | "high";
  effectiveDate: string;
  updatedAt: string;
};

export type CompensationMarketLookup = {
  jobCode?: string;
  level?: string;
  location?: string;
  payZone?: string;
};

const compensationMarketProvider = "Northstar Compensation Data Exchange";

export const COMPENSATION_MARKET_RECORDS: readonly CompensationMarketRecord[] = [
  {
    marketDataId: "cmd_eng_swe3_us_west",
    provider: compensationMarketProvider,
    jobCode: "ENG-SWE3",
    jobTitle: "Senior Software Engineer",
    jobFamily: "Engineering",
    level: "P3",
    location: "San Francisco, CA",
    payZone: "US-WEST",
    currency: "USD",
    annualBaseSalary: {
      p10: 142000,
      p25: 155000,
      p50: 171000,
      p75: 188000,
      p90: 205000,
    },
    annualTotalCash: {
      p10: 156000,
      p25: 171000,
      p50: 190000,
      p75: 211000,
      p90: 235000,
    },
    sampleSize: 842,
    confidence: "high",
    effectiveDate: "2026-01-01",
    updatedAt: "2026-05-01T00:00:00.000Z",
  },
  {
    marketDataId: "cmd_eng_mgr1_us_west",
    provider: compensationMarketProvider,
    jobCode: "ENG-MGR1",
    jobTitle: "Engineering Manager",
    jobFamily: "Engineering Management",
    level: "M1",
    location: "San Francisco, CA",
    payZone: "US-WEST",
    currency: "USD",
    annualBaseSalary: {
      p10: 166000,
      p25: 184000,
      p50: 203000,
      p75: 225000,
      p90: 248000,
    },
    annualTotalCash: {
      p10: 191000,
      p25: 216000,
      p50: 244000,
      p75: 276000,
      p90: 313000,
    },
    sampleSize: 411,
    confidence: "high",
    effectiveDate: "2026-01-01",
    updatedAt: "2026-05-01T00:00:00.000Z",
  },
  {
    marketDataId: "cmd_ppl_hrb2_us_east",
    provider: compensationMarketProvider,
    jobCode: "PPL-HRB2",
    jobTitle: "People Operations Partner",
    jobFamily: "People",
    level: "P2",
    location: "Boston, MA",
    payZone: "US-EAST",
    currency: "USD",
    annualBaseSalary: {
      p10: 92000,
      p25: 104000,
      p50: 116000,
      p75: 129000,
      p90: 142000,
    },
    annualTotalCash: { p10: 97000, p25: 111000, p50: 125000, p75: 141000, p90: 157000 },
    sampleSize: 284,
    confidence: "medium",
    effectiveDate: "2026-01-01",
    updatedAt: "2026-05-01T00:00:00.000Z",
  },
  {
    marketDataId: "cmd_ppl_cmp3_us_east",
    provider: compensationMarketProvider,
    jobCode: "PPL-CMP3",
    jobTitle: "Compensation Program Manager",
    jobFamily: "People",
    level: "P3",
    location: "Boston, MA",
    payZone: "US-EAST",
    currency: "USD",
    annualBaseSalary: {
      p10: 108000,
      p25: 121000,
      p50: 136000,
      p75: 153000,
      p90: 169000,
    },
    annualTotalCash: {
      p10: 119000,
      p25: 134000,
      p50: 152000,
      p75: 173000,
      p90: 194000,
    },
    sampleSize: 197,
    confidence: "medium",
    effectiveDate: "2026-01-01",
    updatedAt: "2026-05-01T00:00:00.000Z",
  },
  {
    marketDataId: "cmd_sal_ae3_us_east",
    provider: compensationMarketProvider,
    jobCode: "SAL-AE3",
    jobTitle: "Account Executive",
    jobFamily: "Sales",
    level: "P3",
    location: "New York, NY",
    payZone: "US-EAST",
    currency: "USD",
    annualBaseSalary: {
      p10: 101000,
      p25: 116000,
      p50: 132000,
      p75: 149000,
      p90: 168000,
    },
    annualTotalCash: {
      p10: 151000,
      p25: 184000,
      p50: 221000,
      p75: 262000,
      p90: 306000,
    },
    sampleSize: 529,
    confidence: "high",
    effectiveDate: "2026-01-01",
    updatedAt: "2026-05-01T00:00:00.000Z",
  },
  {
    marketDataId: "cmd_fin_fpa3_us_central",
    provider: compensationMarketProvider,
    jobCode: "FIN-FPA3",
    jobTitle: "Senior Financial Analyst",
    jobFamily: "Finance",
    level: "P3",
    location: "Austin, TX",
    payZone: "US-CENTRAL",
    currency: "USD",
    annualBaseSalary: {
      p10: 99000,
      p25: 111000,
      p50: 124000,
      p75: 138000,
      p90: 153000,
    },
    annualTotalCash: {
      p10: 110000,
      p25: 124000,
      p50: 140000,
      p75: 158000,
      p90: 178000,
    },
    sampleSize: 346,
    confidence: "high",
    effectiveDate: "2026-01-01",
    updatedAt: "2026-05-01T00:00:00.000Z",
  },
  {
    marketDataId: "cmd_cs_csm3_us_central",
    provider: compensationMarketProvider,
    jobCode: "CS-CSM3",
    jobTitle: "Customer Success Manager",
    jobFamily: "Customer Success",
    level: "P3",
    location: "Denver, CO",
    payZone: "US-CENTRAL",
    currency: "USD",
    annualBaseSalary: {
      p10: 95000,
      p25: 107000,
      p50: 119000,
      p75: 134000,
      p90: 151000,
    },
    annualTotalCash: {
      p10: 110000,
      p25: 126000,
      p50: 144000,
      p75: 166000,
      p90: 190000,
    },
    sampleSize: 271,
    confidence: "medium",
    effectiveDate: "2026-01-01",
    updatedAt: "2026-05-01T00:00:00.000Z",
  },
];

/**
 * Finds the best simulated market match from vendor-style role and location keys.
 */
export function findCompensationMarketRecord(
  lookup: CompensationMarketLookup,
): CompensationMarketRecord | undefined {
  return COMPENSATION_MARKET_RECORDS.find((record) => {
    return (
      optionalNormalizedMatch(record.jobCode, lookup.jobCode) &&
      optionalNormalizedMatch(record.level, lookup.level) &&
      optionalNormalizedMatch(record.location, lookup.location) &&
      optionalNormalizedMatch(record.payZone, lookup.payZone)
    );
  });
}

/**
 * Lists records matching any supplied vendor query dimensions.
 */
export function listCompensationMarketRecords(
  lookup: CompensationMarketLookup,
): CompensationMarketRecord[] {
  return COMPENSATION_MARKET_RECORDS.filter((record) => {
    return (
      optionalNormalizedMatch(record.jobCode, lookup.jobCode) &&
      optionalNormalizedMatch(record.level, lookup.level) &&
      optionalNormalizedMatch(record.location, lookup.location) &&
      optionalNormalizedMatch(record.payZone, lookup.payZone)
    );
  });
}

function optionalNormalizedMatch(recordValue: string, lookupValue: string | undefined) {
  if (lookupValue === undefined) {
    return true;
  }

  return normalizeText(recordValue) === normalizeText(lookupValue);
}

function normalizeText(value: string): string {
  return value.trim().toLowerCase();
}
