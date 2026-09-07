-- Owner: data-plane policy. Phase: P1A.
-- PERFOPT-005: indexes selected by the live foreign-key/catalog audit.
-- Unindexed foreign keys before this migration: 87; referenced indexes added: 65; unreferenced decisions retained in the golden: 22.

-- +goose Up

-- FK balance_entry_tenant_id_definition_id_definition_version_fkey; store internal/data/balancestore/store.go:251
CREATE INDEX IF NOT EXISTS ix_balance_entry_tenant_id_definition_id_definition_version
    ON balance_entry (tenant_id, definition_id, definition_version);

-- FK benefit_plan_revision_tenant_id_supersedes_fkey; store internal/data/benefitsstore/store.go:195
CREATE INDEX IF NOT EXISTS ix_benefit_plan_revision_tenant_id_supersedes
    ON benefit_plan_revision (tenant_id, supersedes);

-- FK config_object_activation_tenant_id_cell_id_kind_object_id__fkey; store internal/data/configregistry/activation.go:38
CREATE INDEX IF NOT EXISTS ix_config_object_activation_tenant_id_cell_id_kind_object_id_re
    ON config_object_activation (tenant_id, cell_id, kind, object_id, revision);

-- FK connector_connection_tenant_id_connector_id_fkey; store internal/data/integrationmeta/store.go:228
CREATE INDEX IF NOT EXISTS ix_connector_connection_tenant_id_connector_id
    ON connector_connection (tenant_id, connector_id);

-- FK connector_operation_tenant_id_causal_predecessor_id_fkey; store internal/data/integrationmeta/store.go:587
CREATE INDEX IF NOT EXISTS ix_connector_operation_tenant_id_causal_predecessor_id
    ON connector_operation (tenant_id, causal_predecessor_id);

-- FK connector_operation_tenant_id_mapping_id_fkey; store internal/data/integrationmeta/store.go:587
CREATE INDEX IF NOT EXISTS ix_connector_operation_tenant_id_mapping_id
    ON connector_operation (tenant_id, mapping_id);

-- FK connector_operation_credential_lease_tenant_id_attempt_id_fkey; store internal/data/connectivityopstore/store.go:300
CREATE INDEX IF NOT EXISTS ix_connector_operation_credential_lease_tenant_id_attempt_id
    ON connector_operation_credential_lease (tenant_id, attempt_id);

-- FK connector_operation_journal_tenant_id_attempt_id_fkey; store internal/data/connectivityopstore/store.go:211
CREATE INDEX IF NOT EXISTS ix_connector_operation_journal_tenant_id_attempt_id
    ON connector_operation_journal (tenant_id, attempt_id);

-- FK custom_record_revision_tenant_id_object_kind_namespace_def_fkey; store internal/data/customstore/store.go:299
CREATE INDEX IF NOT EXISTS ix_custom_record_revision_tenant_id_object_kind_namespace_defin
    ON custom_record_revision (tenant_id, object_kind, namespace, definition_version);

-- FK data_access_manifest_tenant_id_principal_id_fkey; store internal/data/governance/store.go:767
CREATE INDEX IF NOT EXISTS ix_data_access_manifest_tenant_id_principal_id
    ON data_access_manifest (tenant_id, principal_id);

-- FK definition_version_supersession_lineage; store internal/data/balancestore/store.go:298
CREATE INDEX IF NOT EXISTS ix_definition_version_tenant_id_definition_kind_definition_key_
    ON definition_version (tenant_id, definition_kind, definition_key, supersedes_version);

-- FK delivery_attempt_tenant_id_endpoint_id_fkey; store internal/data/messagingmeta/store.go:402
CREATE INDEX IF NOT EXISTS ix_delivery_attempt_tenant_id_endpoint_id
    ON delivery_attempt (tenant_id, endpoint_id);

-- FK document_current_version_fk; store internal/data/documentmeta/store.go:238
CREATE INDEX IF NOT EXISTS ix_document_tenant_id_current_version_id
    ON document (tenant_id, current_version_id);

-- FK document_artifact_reference_tenant_id_version_id_fkey; store internal/data/documentmeta/store.go:541
CREATE INDEX IF NOT EXISTS ix_document_artifact_reference_tenant_id_version_id
    ON document_artifact_reference (tenant_id, version_id);

-- FK document_version_tenant_id_supersedes_version_id_fkey; store internal/data/documentmeta/store.go:257
CREATE INDEX IF NOT EXISTS ix_document_version_tenant_id_supersedes_version_id
    ON document_version (tenant_id, supersedes_version_id);

-- FK document_version_tenant_id_template_id_fkey; store internal/data/documentmeta/store.go:257
CREATE INDEX IF NOT EXISTS ix_document_version_tenant_id_template_id
    ON document_version (tenant_id, template_id);

-- FK entitlement_snapshot_tenant_id_contract_id_revision_fkey; store internal/data/commercialstore/store.go:387
CREATE INDEX IF NOT EXISTS ix_entitlement_snapshot_tenant_id_contract_id_revision
    ON entitlement_snapshot (tenant_id, contract_id, revision);

-- FK equity_grant_tenant_id_plan_id_plan_revision_fkey; store internal/data/equitystore/store.go:314
CREATE INDEX IF NOT EXISTS ix_equity_grant_tenant_id_plan_id_plan_revision
    ON equity_grant (tenant_id, plan_id, plan_revision);

-- FK expected_entitlement_tenant_id_account_link_ref_fkey; store internal/data/accessstore/snapshot.go:150
CREATE INDEX IF NOT EXISTS ix_expected_entitlement_tenant_id_account_link_ref
    ON expected_entitlement (tenant_id, account_link_ref);

-- FK expected_entitlement_tenant_id_employment_ref_fkey; store internal/data/accessstore/snapshot.go:150
CREATE INDEX IF NOT EXISTS ix_expected_entitlement_tenant_id_employment_ref
    ON expected_entitlement (tenant_id, employment_ref);

-- FK expected_entitlement_tenant_id_policy_ref_fkey; store internal/data/accessstore/snapshot.go:150
CREATE INDEX IF NOT EXISTS ix_expected_entitlement_tenant_id_policy_ref
    ON expected_entitlement (tenant_id, policy_ref);

-- FK expected_entitlement_tenant_id_position_ref_fkey; store internal/data/accessstore/snapshot.go:150
CREATE INDEX IF NOT EXISTS ix_expected_entitlement_tenant_id_position_ref
    ON expected_entitlement (tenant_id, position_ref);

-- FK fx_quote_revision_source_fk; store internal/data/fxstore/store.go:301
CREATE INDEX IF NOT EXISTS ix_fx_quote_revision_tenant_id_source_id_source_revision
    ON fx_quote_revision (tenant_id, source_id, source_revision);

-- FK hold_intersection_tenant_id_copy_id_fkey; store internal/data/recordsmeta/store.go:306
CREATE INDEX IF NOT EXISTS ix_hold_intersection_tenant_id_copy_id
    ON hold_intersection (tenant_id, copy_id);

-- FK inbox_record_tenant_id_recipient_message_id_fkey; store internal/data/inbox/store.go:187
CREATE INDEX IF NOT EXISTS ix_inbox_record_tenant_id_recipient_message_id
    ON inbox_record (tenant_id, recipient_message_id);

-- FK integration_receipt_item_tenant_id_mapping_id_fkey; store internal/data/integrationmeta/store.go:496
CREATE INDEX IF NOT EXISTS ix_integration_receipt_item_tenant_id_mapping_id
    ON integration_receipt_item (tenant_id, mapping_id);

-- FK intent_closure_receipt; store internal/data/intentcontrol/transaction.go:1320
CREATE INDEX IF NOT EXISTS ix_intent_closure_tenant_id_execution_receipt_id
    ON intent_closure (tenant_id, execution_receipt_id);

-- FK intent_relationship_child; store internal/data/intentcontrol/facts.go:198
CREATE INDEX IF NOT EXISTS ix_intent_relationship_tenant_id_child_intent_id
    ON intent_relationship (tenant_id, child_intent_id);

-- FK intent_simulation_result_snapshot; store internal/data/intentcontrol/snapshot.go:296
CREATE INDEX IF NOT EXISTS ix_intent_simulation_result_tenant_id_input_snapshot_id
    ON intent_simulation_result (tenant_id, input_snapshot_id);

-- FK ledger_checkpoint_epoch_corrects; store internal/data/ledger/checkpoint/store.go:194
CREATE INDEX IF NOT EXISTS ix_ledger_checkpoint_epoch_tenant_id_corrects_epoch_id
    ON ledger_checkpoint_epoch (tenant_id, corrects_epoch_id);

-- FK ledger_checkpoint_epoch_previous; store internal/data/ledger/checkpoint/store.go:194
CREATE INDEX IF NOT EXISTS ix_ledger_checkpoint_epoch_tenant_id_previous_epoch_id
    ON ledger_checkpoint_epoch (tenant_id, previous_epoch_id);

-- FK ledger_checkpoint_stream_head_stream; store internal/data/ledger/checkpoint/store.go:167
CREATE INDEX IF NOT EXISTS ix_ledger_checkpoint_stream_head_tenant_id_stream_key
    ON ledger_checkpoint_stream_head (tenant_id, stream_key);

-- FK ledger_event_authority; store internal/data/bitemporal/sql.go:32
CREATE INDEX IF NOT EXISTS ix_ledger_event_tenant_id_authority_ref
    ON ledger_event (tenant_id, authority_ref);

-- FK ledger_event_schema; store internal/data/bitemporal/sql.go:32
CREATE INDEX IF NOT EXISTS ix_ledger_event_tenant_id_schema_ref
    ON ledger_event (tenant_id, schema_ref);

-- FK legal_rule_pack_jurisdiction_id_fkey; store internal/data/governance/store.go:813
CREATE INDEX IF NOT EXISTS ix_legal_rule_pack_jurisdiction_id
    ON legal_rule_pack (jurisdiction_id);

-- FK mapping_execution_tenant_id_mapping_id_fkey; store internal/data/integrationmeta/store.go:357
CREATE INDEX IF NOT EXISTS ix_mapping_execution_tenant_id_mapping_id
    ON mapping_execution (tenant_id, mapping_id);

-- FK mapping_profile_tenant_id_connection_id_fkey; store internal/data/integrationmeta/store.go:298
CREATE INDEX IF NOT EXISTS ix_mapping_profile_tenant_id_connection_id
    ON mapping_profile (tenant_id, connection_id);

-- FK merit_cycle_population_fk; store internal/data/meritstore/store.go:184
CREATE INDEX IF NOT EXISTS ix_merit_cycle_revision_tenant_id_population_ref
    ON merit_cycle_revision (tenant_id, population_ref);

-- FK merit_recommendation_cycle_fk; store internal/data/meritstore/store.go:358
CREATE INDEX IF NOT EXISTS ix_merit_recommendation_tenant_id_cycle_id_cycle_revision
    ON merit_recommendation (tenant_id, cycle_id, cycle_revision);

-- FK obligation_binding_tenant_id_principal_id_fkey; store internal/data/governance/store.go:889
CREATE INDEX IF NOT EXISTS ix_obligation_binding_tenant_id_principal_id
    ON obligation_binding (tenant_id, principal_id);

-- FK outbox_schema; store internal/data/health/probe.go:354
CREATE INDEX IF NOT EXISTS ix_outbox_tenant_id_schema_ref
    ON outbox (tenant_id, schema_ref);

-- FK partner_installation_event_tenant_id_installation_id_revis_fkey; store internal/data/commercialstore/store.go:601
CREATE INDEX IF NOT EXISTS ix_partner_installation_event_tenant_id_installation_id_revisio
    ON partner_installation_event (tenant_id, installation_id, revision);

-- FK payinput_worker_assignment_definition_fk; store internal/data/payinputstore/store.go:455
CREATE INDEX IF NOT EXISTS ix_payinput_worker_assignment_tenant_id_definition_ref_definiti
    ON payinput_worker_assignment (tenant_id, definition_ref, definition_revision);

-- FK payroll_frozen_population_run_revision_fk; store internal/data/payrollstore/store.go:217
CREATE INDEX IF NOT EXISTS ix_payroll_frozen_population_tenant_id_run_id_run_revision
    ON payroll_frozen_population (tenant_id, run_id, run_revision);

-- FK projection_checkpoint_stream; store internal/data/health/probe.go:231
CREATE INDEX IF NOT EXISTS ix_projection_checkpoint_tenant_id_stream_key
    ON projection_checkpoint (tenant_id, stream_key);

-- FK recipient_message_tenant_id_endpoint_id_fkey; store internal/data/inboundmsg/store.go:378
CREATE INDEX IF NOT EXISTS ix_recipient_message_tenant_id_endpoint_id
    ON recipient_message (tenant_id, endpoint_id);

-- FK recipient_message_satisfying_receipt_fk; store internal/data/inboundmsg/store.go:378
CREATE INDEX IF NOT EXISTS ix_recipient_message_tenant_id_satisfying_receipt_id
    ON recipient_message (tenant_id, satisfying_receipt_id);

-- FK reconciliation_job_tenant_id_connection_id_fkey; store internal/data/integrationmeta/store.go:818
CREATE INDEX IF NOT EXISTS ix_reconciliation_job_tenant_id_connection_id
    ON reconciliation_job (tenant_id, connection_id);

-- FK record_declaration_tenant_id_corrects_declaration_id_fkey; store internal/data/recordsmeta/store.go:170
CREATE INDEX IF NOT EXISTS ix_record_declaration_tenant_id_corrects_declaration_id
    ON record_declaration (tenant_id, corrects_declaration_id);

-- FK recovery_run_tenant_id_backup_run_id_fkey; store internal/data/opsmeta/store.go:366
CREATE INDEX IF NOT EXISTS ix_recovery_run_tenant_id_backup_run_id
    ON recovery_run (tenant_id, backup_run_id);

-- FK recovery_run_tenant_id_incident_id_fkey; store internal/data/opsmeta/store.go:366
CREATE INDEX IF NOT EXISTS ix_recovery_run_tenant_id_incident_id
    ON recovery_run (tenant_id, incident_id);

-- FK reference_dataset_adoption_dataset_id_version_fkey; store internal/data/refdata/store.go:298
CREATE INDEX IF NOT EXISTS ix_reference_dataset_adoption_dataset_id_version
    ON reference_dataset_adoption (dataset_id, version);

-- FK reply_binding_tenant_id_recipient_message_id_fkey; store internal/data/inboundmsg/store.go:297
CREATE INDEX IF NOT EXISTS ix_reply_binding_tenant_id_recipient_message_id
    ON reply_binding (tenant_id, recipient_message_id);

-- FK retention_disposition_tenant_id_blocking_hold_id_fkey; store internal/data/recordsmeta/store.go:432
CREATE INDEX IF NOT EXISTS ix_retention_disposition_tenant_id_blocking_hold_id
    ON retention_disposition (tenant_id, blocking_hold_id);

-- FK retention_disposition_tenant_id_corrects_disposition_id_fkey; store internal/data/recordsmeta/store.go:432
CREATE INDEX IF NOT EXISTS ix_retention_disposition_tenant_id_corrects_disposition_id
    ON retention_disposition (tenant_id, corrects_disposition_id);

-- FK role_organization_visibility_tenant_id_role_id_fkey; store internal/data/roleaccessstore/store.go:96
CREATE INDEX IF NOT EXISTS ix_role_organization_visibility_tenant_id_role_id
    ON role_organization_visibility (tenant_id, role_id);

-- FK scenario_revision_parent_same_scenario; store internal/data/planningstore/store.go:891
CREATE INDEX IF NOT EXISTS ix_scenario_revision_tenant_id_scenario_id_parent_revision
    ON scenario_revision (tenant_id, scenario_id, parent_revision);

-- FK signature_tenant_id_version_id_signed_digest_fkey; store internal/data/documentmeta/store.go:473
CREATE INDEX IF NOT EXISTS ix_signature_tenant_id_version_id_signed_digest
    ON signature (tenant_id, version_id, signed_digest);

-- FK signature_request_tenant_id_version_id_fkey; store internal/data/documentmeta/store.go:405
CREATE INDEX IF NOT EXISTS ix_signature_request_tenant_id_version_id
    ON signature_request (tenant_id, version_id);

-- FK subscription_authorization_event_subscription_fk; store internal/data/subscriptionstore/store.go:291
CREATE INDEX IF NOT EXISTS ix_subscription_authorization_event_tenant_id_subscription_id_r
    ON subscription_authorization_event (tenant_id, subscription_id, revision);

-- FK succession_slate_critical_role_revision; store internal/data/successionstore/store.go:458
CREATE INDEX IF NOT EXISTS ix_succession_slate_tenant_id_critical_role_id_critical_role_re
    ON succession_slate (tenant_id, critical_role_id, critical_role_revision);

-- FK worker_access_role_assignment_tenant_id_role_id_fkey; store internal/data/roleaccessstore/store.go:71
CREATE INDEX IF NOT EXISTS ix_worker_access_role_assignment_tenant_id_role_id
    ON worker_access_role_assignment (tenant_id, role_id);

-- FK workflow_approval_requirement_tenant_id_work_item_id_fkey; store internal/data/runtimestate/workqueue.go:638
CREATE INDEX IF NOT EXISTS ix_workflow_approval_requirement_tenant_id_work_item_id
    ON workflow_approval_requirement (tenant_id, work_item_id);

-- FK workflow_signal_receipt_tenant_id_instance_id_fkey; store internal/data/signals/store.go:290
CREATE INDEX IF NOT EXISTS ix_workflow_signal_receipt_tenant_id_instance_id
    ON workflow_signal_receipt (tenant_id, instance_id);

-- FK workflow_signal_receipt_tenant_id_subscription_id_fkey; store internal/data/signals/store.go:290
CREATE INDEX IF NOT EXISTS ix_workflow_signal_receipt_tenant_id_subscription_id
    ON workflow_signal_receipt (tenant_id, subscription_id);

-- +goose Down

DROP INDEX IF EXISTS ix_balance_entry_tenant_id_definition_id_definition_version;
DROP INDEX IF EXISTS ix_benefit_plan_revision_tenant_id_supersedes;
DROP INDEX IF EXISTS ix_config_object_activation_tenant_id_cell_id_kind_object_id_re;
DROP INDEX IF EXISTS ix_connector_connection_tenant_id_connector_id;
DROP INDEX IF EXISTS ix_connector_operation_tenant_id_causal_predecessor_id;
DROP INDEX IF EXISTS ix_connector_operation_tenant_id_mapping_id;
DROP INDEX IF EXISTS ix_connector_operation_credential_lease_tenant_id_attempt_id;
DROP INDEX IF EXISTS ix_connector_operation_journal_tenant_id_attempt_id;
DROP INDEX IF EXISTS ix_custom_record_revision_tenant_id_object_kind_namespace_defin;
DROP INDEX IF EXISTS ix_data_access_manifest_tenant_id_principal_id;
DROP INDEX IF EXISTS ix_definition_version_tenant_id_definition_kind_definition_key_;
DROP INDEX IF EXISTS ix_delivery_attempt_tenant_id_endpoint_id;
DROP INDEX IF EXISTS ix_document_tenant_id_current_version_id;
DROP INDEX IF EXISTS ix_document_artifact_reference_tenant_id_version_id;
DROP INDEX IF EXISTS ix_document_version_tenant_id_supersedes_version_id;
DROP INDEX IF EXISTS ix_document_version_tenant_id_template_id;
DROP INDEX IF EXISTS ix_entitlement_snapshot_tenant_id_contract_id_revision;
DROP INDEX IF EXISTS ix_equity_grant_tenant_id_plan_id_plan_revision;
DROP INDEX IF EXISTS ix_expected_entitlement_tenant_id_account_link_ref;
DROP INDEX IF EXISTS ix_expected_entitlement_tenant_id_employment_ref;
DROP INDEX IF EXISTS ix_expected_entitlement_tenant_id_policy_ref;
DROP INDEX IF EXISTS ix_expected_entitlement_tenant_id_position_ref;
DROP INDEX IF EXISTS ix_fx_quote_revision_tenant_id_source_id_source_revision;
DROP INDEX IF EXISTS ix_hold_intersection_tenant_id_copy_id;
DROP INDEX IF EXISTS ix_inbox_record_tenant_id_recipient_message_id;
DROP INDEX IF EXISTS ix_integration_receipt_item_tenant_id_mapping_id;
DROP INDEX IF EXISTS ix_intent_closure_tenant_id_execution_receipt_id;
DROP INDEX IF EXISTS ix_intent_relationship_tenant_id_child_intent_id;
DROP INDEX IF EXISTS ix_intent_simulation_result_tenant_id_input_snapshot_id;
DROP INDEX IF EXISTS ix_ledger_checkpoint_epoch_tenant_id_corrects_epoch_id;
DROP INDEX IF EXISTS ix_ledger_checkpoint_epoch_tenant_id_previous_epoch_id;
DROP INDEX IF EXISTS ix_ledger_checkpoint_stream_head_tenant_id_stream_key;
DROP INDEX IF EXISTS ix_ledger_event_tenant_id_authority_ref;
DROP INDEX IF EXISTS ix_ledger_event_tenant_id_schema_ref;
DROP INDEX IF EXISTS ix_legal_rule_pack_jurisdiction_id;
DROP INDEX IF EXISTS ix_mapping_execution_tenant_id_mapping_id;
DROP INDEX IF EXISTS ix_mapping_profile_tenant_id_connection_id;
DROP INDEX IF EXISTS ix_merit_cycle_revision_tenant_id_population_ref;
DROP INDEX IF EXISTS ix_merit_recommendation_tenant_id_cycle_id_cycle_revision;
DROP INDEX IF EXISTS ix_obligation_binding_tenant_id_principal_id;
DROP INDEX IF EXISTS ix_outbox_tenant_id_schema_ref;
DROP INDEX IF EXISTS ix_partner_installation_event_tenant_id_installation_id_revisio;
DROP INDEX IF EXISTS ix_payinput_worker_assignment_tenant_id_definition_ref_definiti;
DROP INDEX IF EXISTS ix_payroll_frozen_population_tenant_id_run_id_run_revision;
DROP INDEX IF EXISTS ix_projection_checkpoint_tenant_id_stream_key;
DROP INDEX IF EXISTS ix_recipient_message_tenant_id_endpoint_id;
DROP INDEX IF EXISTS ix_recipient_message_tenant_id_satisfying_receipt_id;
DROP INDEX IF EXISTS ix_reconciliation_job_tenant_id_connection_id;
DROP INDEX IF EXISTS ix_record_declaration_tenant_id_corrects_declaration_id;
DROP INDEX IF EXISTS ix_recovery_run_tenant_id_backup_run_id;
DROP INDEX IF EXISTS ix_recovery_run_tenant_id_incident_id;
DROP INDEX IF EXISTS ix_reference_dataset_adoption_dataset_id_version;
DROP INDEX IF EXISTS ix_reply_binding_tenant_id_recipient_message_id;
DROP INDEX IF EXISTS ix_retention_disposition_tenant_id_blocking_hold_id;
DROP INDEX IF EXISTS ix_retention_disposition_tenant_id_corrects_disposition_id;
DROP INDEX IF EXISTS ix_role_organization_visibility_tenant_id_role_id;
DROP INDEX IF EXISTS ix_scenario_revision_tenant_id_scenario_id_parent_revision;
DROP INDEX IF EXISTS ix_signature_tenant_id_version_id_signed_digest;
DROP INDEX IF EXISTS ix_signature_request_tenant_id_version_id;
DROP INDEX IF EXISTS ix_subscription_authorization_event_tenant_id_subscription_id_r;
DROP INDEX IF EXISTS ix_succession_slate_tenant_id_critical_role_id_critical_role_re;
DROP INDEX IF EXISTS ix_worker_access_role_assignment_tenant_id_role_id;
DROP INDEX IF EXISTS ix_workflow_approval_requirement_tenant_id_work_item_id;
DROP INDEX IF EXISTS ix_workflow_signal_receipt_tenant_id_instance_id;
DROP INDEX IF EXISTS ix_workflow_signal_receipt_tenant_id_subscription_id;
