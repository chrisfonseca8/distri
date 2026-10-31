variable "aws_region" {
  description = "AWS region; keep it aligned with the Neon project."
  type        = string
  default     = "us-east-1"
}

variable "availability_zone" {
  description = "Single staging AZ."
  type        = string
  default     = "us-east-1a"
}

variable "project_name" {
  description = "Resource-name prefix."
  type        = string
  default     = "distri-staging"
}

variable "vpc_cidr" {
  type    = string
  default = "10.40.0.0/16"
}

variable "public_subnet_cidr" {
  type    = string
  default = "10.40.1.0/24"
}

variable "admin_cidr" {
  description = "Optional administrator public CIDR for SSH and RabbitMQ UI, e.g. 203.0.113.10/32. Empty disables it."
  type        = string
  default     = ""

  validation {
    condition     = var.admin_cidr == "" || can(cidrhost(var.admin_cidr, 0))
    error_message = "admin_cidr must be empty or a valid CIDR."
  }
}

variable "additional_admin_cidrs" {
  description = "Additional trusted administrator CIDRs for connections that use more than one egress address."
  type        = set(string)
  default     = []

  validation {
    condition     = alltrue([for cidr in var.additional_admin_cidrs : can(cidrhost(cidr, 0))])
    error_message = "Every additional_admin_cidrs entry must be a valid CIDR."
  }
}

variable "key_name" {
  description = "Optional existing EC2 key-pair name. Required for the SSH Docker-socket tunnel."
  type        = string
  default     = null
  nullable    = true
}

variable "create_instance_profile" {
  description = "Create and attach the SSM instance profile. Disable when the provisioning principal has EC2 access but no IAM permissions."
  type        = bool
  default     = true
}

variable "create_elastic_ips" {
  description = "Allocate stable public IPv4 addresses. Disable when the regional EIP quota is lower than the six-host topology."
  type        = bool
  default     = true
}

variable "service_instance_type" {
  description = "Instance type used independently for orchestrator, order, inventory, and payment."
  type        = string
  default     = "c7i.large"
}

variable "broker_instance_type" {
  type    = string
  default = "m7i.large"
}

variable "loadgen_instance_type" {
  type    = string
  default = "c7i.large"
}

variable "service_root_gib" {
  type    = number
  default = 30
}

variable "broker_root_gib" {
  type    = number
  default = 20
}

variable "broker_data_gib" {
  type    = number
  default = 50
}

variable "loadgen_root_gib" {
  type    = number
  default = 40
}

variable "docker_compose_version" {
  description = "Pinned Docker Compose release installed by cloud-init."
  type        = string
  default     = "v2.40.3"
}

variable "budget_alert_email" {
  description = "Optional email for an AWS monthly cost budget. Empty disables budget creation."
  type        = string
  default     = ""
  sensitive   = true
}

variable "monthly_budget_usd" {
  type    = number
  default = 25
}
