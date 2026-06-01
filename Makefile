.PHONY: infra-apply infra-destroy infra-plan

infra-plan:
	cd infra/terraform && terraform init -input=false && terraform plan

infra-apply:
	cd infra/terraform && terraform init -input=false && terraform apply -auto-approve

infra-destroy:
	cd infra/terraform && terraform init -input=false && terraform destroy -auto-approve
