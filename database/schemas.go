package database

// Employee Schema
var EmployeSchema = `{
  "$jsonSchema": {
    "bsonType": "object",
    "required": ["employeeId", "firstName", "lastName", "email", "socialSecurityNumber", "personalDetails", "jobHistory", "statusHistory", "compensationDetails"],
    "properties": {
      "employeeId": {
        "bsonType": "string",
        "description": "must be a string and is required"
      },
      "preferredName": {
        "bsonType": "string",
        "description": "must be a string if provided"
      },
      "firstName": {
        "bsonType": "string",
        "description": "must be a string and is required"
      },
      "middleName": {
        "bsonType": "string",
        "description": "must be a string if provided"
      },
      "lastName": {
        "bsonType": "string",
        "description": "must be a string and is required"
      },
      "suffix": {
        "bsonType": "string",
        "description": "must be a string if provided"
      },
      "email": {
        "bsonType": "string",
        "pattern": "^.+@.+\\..+$",
        "description": "must be a valid email address and is required"
      },
      "phone": {
        "bsonType": "string",
        "pattern": "^\\+[1-9]\\d{1,14}$",
        "description": "must be a valid phone number in E.164 format"
      },
      "socialSecurityNumber": {
        "bsonType": "string",
        "pattern": "^(\\d{3}-\\d{2}-\\d{4})$",
        "description": "must be a valid SSN in the format XXX-XX-XXXX and is required"
      },
      "personalDetails": {
        "bsonType": "object",
        "required": ["dateOfBirth", "gender", "maritalStatus", "address", "emergencyContacts"],
        "properties": {
          "preferredGender": {
            "bsonType": "string",
            "enum": ["He/Him", "She/Her", "They/Them", "Other"],
            "description": "must be one of the predefined values if provided"
          },
          "dateOfBirth": {
            "bsonType": "date",
            "description": "must be a valid date and is required"
          },
          "gender": {
            "bsonType": "string",
            "enum": ["Male", "Female", "Other"],
            "description": "must be one of the predefined values and is required"
          },
          "maritalStatus": {
            "bsonType": "string",
            "enum": ["Single", "Married", "Divorced", "Widowed"],
            "description": "must be one of the predefined values and is required"
          },
          "nationality": {
            "bsonType": "string",
            "description": "must be a string if provided"
          },
          "placeOfBirth": {
            "bsonType": "string",
            "description": "must be a string if provided"
          },
          "address": {
            "bsonType": "object",
            "required": ["street", "city", "state", "zipCode", "country"],
            "properties": {
              "street": {
                "bsonType": "string",
                "description": "must be a string and is required"
              },
              "city": {
                "bsonType": "string",
                "description": "must be a string and is required"
              },
              "state": {
                "bsonType": "string",
                "description": "must be a string and is required"
              },
              "zipCode": {
                "bsonType": "string",
                "description": "must be a string and is required"
              },
              "country": {
                "bsonType": "string",
                "description": "must be a string and is required"
              }
            }
          },
          "emergencyContacts": {
            "bsonType": "array",
            "minItems": 1,
            "items": {
              "bsonType": "object",
              "required": ["name", "relation", "phone"],
              "properties": {
                "name": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                },
                "relation": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                },
                "phone": {
                  "bsonType": "string",
                  "pattern": "^\\+[1-9]\\d{1,14}$",
                  "description": "must be a valid phone number in E.164 format and is required"
                },
                "email": {
                  "bsonType": "string",
                  "pattern": "^.+@.+\\..+$",
                  "description": "must be a valid email address if provided"
                },
                "address": {
                  "bsonType": "object",
                  "properties": {
                    "street": {
                      "bsonType": "string",
                      "description": "must be a string if provided"
                    },
                    "city": {
                      "bsonType": "string",
                      "description": "must be a string if provided"
                    },
                    "state": {
                      "bsonType": "string",
                      "description": "must be a string if provided"
                    },
                    "zipCode": {
                      "bsonType": "string",
                      "description": "must be a string if provided"
                    },
                    "country": {
                      "bsonType": "string",
                      "description": "must be a string if provided"
                    }
                  }
                }
              }
            }
          }
        }
      },
      "jobHistory": {
        "bsonType": "array",
        "minItems": 1,
        "items": {
          "bsonType": "object",
          "required": ["jobId", "title", "department", "startDate", "location", "employmentType"],
          "properties": {
            "jobId": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "title": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "department": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "startDate": {
              "bsonType": "date",
              "description": "must be a valid date and is required"
            },
            "endDate": {
              "bsonType": ["date", "null"],
              "description": "must be a valid date if provided"
            },
            "location": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "employmentType": {
              "bsonType": "string",
              "enum": ["Full-time", "Part-time", "Contract", "Temporary"],
              "description": "must be one of the predefined values and is required"
            },
            "manager": {
              "bsonType": "object",
              "properties": {
                "name": {
                  "bsonType": "string",
                  "description": "must be a string if provided"
                },
                "employeeId": {
                  "bsonType": "string",
                  "description": "must be a string if provided"
                },
                "email": {
                  "bsonType": "string",
                  "pattern": "^.+@.+\\..+$",
                  "description": "must be a valid email address if provided"
                }
              }
            },
            "responsibilities": {
              "bsonType": "array",
              "items": {
                "bsonType": "string",
                "description": "must be a string"
              }
            }
          }
        }
      },
      "statusHistory": {
        "bsonType": "array",
        "minItems": 1,
        "items": {
          "bsonType": "object",
          "required": ["status", "date"],
          "properties": {
            "status": {
              "bsonType": "string",
              "enum": ["Active", "Leave of Absence", "Terminated", "Retired", "Remote Work"],
              "description": "must be one of the predefined values and is required"
            },
            "date": {
              "bsonType": "date",
              "description": "must be a valid date and is required"
            },
            "reason": {
              "bsonType": "string",
              "description": "must be a string if provided"
            }
          }
        }
      },
      "compensationDetails": {
        "bsonType": "array",
        "minItems": 1,
        "items": {
          "bsonType": "object",
          "required": ["effectiveDate", "salary", "currency", "payFrequency"],
          "properties": {
            "effectiveDate": {
              "bsonType": "date",
              "description": "must be a valid date and is required"
            },
            "salary": {
              "bsonType": "number",
              "minimum": 0,
              "description": "must be a non-negative number and is required"
            },
            "currency": {
              "bsonType": "string",
              "enum": ["USD", "EUR", "GBP", "JPY", "AUD", "CAD"],
              "description": "must be one of the predefined values and is required"
            },
            "payFrequency": {
              "bsonType": "string",
              "enum": ["Weekly", "Bi-weekly", "Monthly", "Annually"],
              "description": "must be one of the predefined values and is required"
            },
            "bonuses": {
              "bsonType": "array",
              "items": {
                "bsonType": "object",
                "required": ["amount", "currency", "date"],
                "properties": {
                  "amount": {
                    "bsonType": "number",
                    "minimum": 0,
                    "description": "must be a non-negative number and is required"
                  },
                  "currency": {
                    "bsonType": "string",
                    "enum": ["USD", "EUR", "GBP", "JPY", "AUD", "CAD"],
                    "description": "must be one of the predefined values and is required"
                  },
                  "date": {
                    "bsonType": "date",
                    "description": "must be a valid date and is required"
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`

// Job schema
var JobSchema = `{
  "$jsonSchema": {
    "bsonType": "object",
    "required": ["jobId", "jobName", "positions", "locations", "budget", "headcount", "creationDate"],
    "properties": {
      "jobId": {
        "bsonType": "string",
        "description": "must be a string and is required"
      },
      "jobName": {
        "bsonType": "string",
        "description": "must be a string and is required"
      },
      "jobDescription": {
        "bsonType": "string",
        "description": "must be a string"
      },
      "positions": {
        "bsonType": "array",
        "minItems": 1,
        "items": {
          "bsonType": "object",
          "required": ["title", "role", "level", "employmentType", "skillsRequired", "experienceRequired"],
          "properties": {
            "title": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "role": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "level": {
              "bsonType": "string",
              "enum": ["Junior", "Mid-level", "Senior", "Lead"],
              "description": "must be one of the predefined values and is required"
            },
            "employmentType": {
              "bsonType": "string",
              "enum": ["Full-time", "Part-time", "Contract", "Internship"],
              "description": "must be one of the predefined values and is required"
            },
            "skillsRequired": {
              "bsonType": "array",
              "minItems": 1,
              "items": {
                "bsonType": "string"
              },
              "description": "must be an array of strings and is required"
            },
            "experienceRequired": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "certifications": {
              "bsonType": "array",
              "items": {
                "bsonType": "string"
              },
              "description": "optional array of strings"
            }
          }
        },
        "description": "must be an array of position objects and is required"
      },
      "locations": {
        "bsonType": "array",
        "minItems": 1,
        "items": {
          "bsonType": "object",
          "required": ["officeName", "address", "remoteEligible", "timeZone"],
          "properties": {
            "officeName": {
              "bsonType": "string",
              "description": "must be a string and is required"
            },
            "address": {
              "bsonType": "object",
              "required": ["street", "city", "state", "zipCode", "country"],
              "properties": {
                "street": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                },
                "city": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                },
                "state": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                },
                "zipCode": {
                  "bsonType": "string",
                  "pattern": "^[0-9]{5}(?:-[0-9]{4})?$",
                  "description": "must be a valid zip code and is required"
                },
                "country": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                }
              },
              "description": "must be an object and is required"
            },
            "remoteEligible": {
              "bsonType": "bool",
              "description": "must be a boolean and is required"
            },
            "timeZone": {
              "bsonType": "string",
              "description": "must be a string and is required"
            }
          }
        },
        "description": "must be an array of location objects and is required"
      },
      "budget": {
        "bsonType": "object",
        "required": ["totalBudget", "currency", "allocation"],
        "properties": {
          "totalBudget": {
            "bsonType": "number",
            "minimum": 0,
            "description": "must be a non-negative number and is required"
          },
          "currency": {
            "bsonType": "string",
            "enum": ["USD", "EUR", "GBP", "JPY", "AUD", "CAD"],
            "description": "must be one of the predefined values and is required"
          },
          "allocation": {
            "bsonType": "object",
            "required": ["salary", "benefits", "equipment"],
            "properties": {
              "salary": {
                "bsonType": "number",
                "minimum": 0,
                "description": "must be a non-negative number and is required"
              },
              "benefits": {
                "bsonType": "number",
                "minimum": 0,
                "description": "must be a non-negative number and is required"
              },
              "equipment": {
                "bsonType": "number",
                "minimum": 0,
                "description": "must be a non-negative number and is required"
              }
            },
            "description": "must be an object containing budget allocations and is required"
          },
          "budgetNotes": {
            "bsonType": "string",
            "description": "optional field for budget notes"
          }
        },
        "description": "must be an object and is required"
      },
      "headcount": {
        "bsonType": "object",
        "required": ["currentHeadcount", "targetHeadcount"],
        "properties": {
          "currentHeadcount": {
            "bsonType": "int",
            "minimum": 0,
            "description": "must be a non-negative integer and is required"
          },
          "targetHeadcount": {
            "bsonType": "int",
            "minimum": 0,
            "description": "must be a non-negative integer and is required"
          },
          "positionsFilled": {
            "bsonType": "array",
            "items": {
              "bsonType": "object",
              "required": ["positionTitle", "employeeId", "employeeName"],
              "properties": {
                "positionTitle": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                },
                "employeeId": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                },
                "employeeName": {
                  "bsonType": "string",
                  "description": "must be a string and is required"
                }
              }
            },
            "description": "optional array of filled position objects"
          }
        },
        "description": "must be an object and is required"
      },
      "jobPostingDetails": {
        "bsonType": "object",
        "properties": {
          "postedDate": {
            "bsonType": "date",
            "description": "must be a valid date"
          },
          "postingStatus": {
            "bsonType": "string",
            "enum": ["Active", "Closed", "Draft"],
            "description": "must be one of the predefined values"
          },
          "closingDate": {
            "bsonType": "date",
            "description": "must be a valid date"
          },
          "applicationDeadline": {
            "bsonType": "date",
            "description": "must be a valid date"
          },
          "jobBoards": {
            "bsonType": "array",
            "items": {
              "bsonType": "string"
            },
            "description": "optional array of job boards"
          },
          "recruiter": {
            "bsonType": "object",
            "required": ["recruiterName", "recruiterEmail"],
            "properties": {
              "recruiterName": {
                "bsonType": "string",
                "description": "must be a string and is required"
              },
              "recruiterEmail": {
                "bsonType": "string",
                "pattern": "^\\S+@\\S+\\.\\S+$",
                "description": "must be a valid email address and is required"
              },
              "recruiterPhone": {
                "bsonType": "string",
                "description": "optional string for recruiter phone number"
              }
            },
            "description": "must be an object and is required"
          }
        },
        "description": "optional object containing job posting details"
      },
      "jobRequirements": {
        "bsonType": "object",
        "properties": {
          "educationLevel": {
            "bsonType": "string",
            "description": "optional string for education level"
          },
          "languagesRequired": {
            "bsonType": "array",
            "items": {
              "bsonType": "string"
            },
            "description": "optional array of strings"
          },
          "travelRequirements": {
            "bsonType": "string",
            "description": "optional string for travel requirements"
          },
          "clearanceLevel": {
            "bsonType": "string",
            "enum": ["None", "Confidential", "Secret", "Top Secret"],
            "description": "must be one of the predefined values"
          }
        },
        "description": "optional object containing job requirements"
      },
      "creationDate": {
        "bsonType": "date",
        "description": "must be a valid date and is required"
      },
      "lastModifiedDate": {
        "bsonType": "date",
        "description": "optional valid date for last modification"
      },
      "notes": {
        "bsonType": "string",
        "description": "optional field for additional notes"
      }
    }
  }
}`
