# Molecule Testing Guide

A comprehensive guide to writing quality Molecule tests for Ansible roles and playbooks.
This document serves as the knowledge base for the ansible-molecule agent.

---

## Table of Contents

- [Overview](#overview)
- [Testing Philosophy](#testing-philosophy)
- [Scenario Structure](#scenario-structure)
- [molecule.yml Configuration](#moleculeyml-configuration)
- [Converge Playbooks](#converge-playbooks)
- [Verify Playbooks](#verify-playbooks)
- [Multi-Platform Testing](#multi-platform-testing)
- [Idempotence Testing](#idempotence-testing)
- [Prepare and Cleanup](#prepare-and-cleanup)
- [Side Effects and Advanced Patterns](#side-effects-and-advanced-patterns)
- [Development Workflow](#development-workflow)
- [CI/CD Integration](#cicd-integration)
- [Common Anti-Patterns](#common-anti-patterns)
- [Quality Checklist](#quality-checklist)

---

## Overview

Molecule provides a framework for developing and testing Ansible roles and playbooks.
It enables:

- **Reproducible testing** - consistent test environments via containers or VMs
- **Multi-platform validation** - test across different OS families
- **Idempotence verification** - ensure roles can run multiple times safely
- **Assertion-based verification** - programmatic validation of role outcomes

### Core Principles

1. **Every role needs tests** - untested code is unreliable code
2. **Tests must assert outcomes** - checking existence is not enough
3. **Idempotence is mandatory** - roles must be safe to run repeatedly
4. **Multi-platform coverage** - test all supported platforms
5. **Fast feedback** - tests should run quickly in CI

---

## Testing Philosophy

Molecule implements the **Four-Phase Test Pattern** - a structured approach that makes
test objectives clear:

1. **Setup** (create/prepare) - Provision clean, isolated test environments
2. **Exercise** (converge) - Execute the Ansible content being tested
3. **Verify** (verify) - Validate the desired outcomes were achieved
4. **Teardown** (destroy) - Remove all created artifacts

### BDD Workflow (Given-When-Then)

The recommended development workflow maps to BDD:

| BDD Phase | Molecule Command | Purpose |
|-----------|------------------|---------|
| **Given** | `molecule create` | Environment is provisioned |
| **When** | `molecule converge` | Role/playbook is applied |
| **Then** | `molecule verify` | Outcomes are validated |

### Test Isolation and Reproducibility

Each test must run in predictable, isolated conditions that:

- Don't interfere with other tests or external systems
- Produce consistent results across different environments
- Work identically for all team members and CI/CD pipelines

### Resource Lifecycle

Molecule manages the complete lifecycle:

1. **Environment provisioning** - Create clean, isolated test environments
2. **Dependency resolution** - Ensure all required resources are available
3. **Change application** - Execute the system logic being tested
4. **Idempotence verification** - Confirm operations produce no unintended changes
5. **Functional verification** - Validate desired outcomes were achieved
6. **Side effect detection** - Identify unintended consequences
7. **Resource cleanup** - Remove all created artifacts

---

## Scenario Structure

A Molecule scenario defines a complete test environment and sequence.

### Directory Layout

```
roles/my_role/
└── molecule/
    ├── default/                    # Default scenario (required)
    │   ├── molecule.yml            # Scenario configuration
    │   ├── converge.yml            # Playbook to apply the role
    │   ├── verify.yml              # Verification playbook
    │   ├── prepare.yml             # Pre-test setup (optional)
    │   ├── cleanup.yml             # Post-test cleanup (optional)
    │   ├── side_effect.yml         # Side effect playbook (optional)
    │   └── requirements.yml        # Role/collection dependencies
    └── security/                   # Additional scenario
        ├── molecule.yml
        ├── converge.yml
        └── verify.yml
```

### File Purposes

| File | Purpose |
|------|---------|
| `molecule.yml` | Scenario configuration: platforms, provisioner, test sequence |
| `converge.yml` | Applies the role under test with test variables |
| `verify.yml` | Validates the role produced the expected outcomes |
| `prepare.yml` | Sets up prerequisites before converge (optional) |
| `cleanup.yml` | Tears down resources after tests (optional) |
| `requirements.yml` | Dependencies for the scenario |

### Multiple Scenarios

Use multiple scenarios for different test purposes:

```
molecule/
├── default/        # Standard functionality
├── security/       # Security-focused tests
├── upgrade/        # Upgrade path testing
└── minimal/        # Minimal configuration
```

---

## molecule.yml Configuration

The `molecule.yml` file is the central configuration entrypoint for Molecule.

### Configuration Hierarchy

Settings are inherited from multiple levels (most specific wins):

1. `$HOME/.config/molecule/config.yml` - Global defaults for all projects
2. Project root `config.yml` - Project-level defaults
3. `extensions/molecule/config.yml` - Collection-specific defaults
4. `molecule/*/molecule.yml` - Scenario-specific overrides

Empty `molecule.yml` files inherit the complete configuration from parent configs.

### Environment Variable Substitution

Molecule supports shell-style variable substitution in `molecule.yml`:

```yaml
platforms:
  - name: ${MOLECULE_INSTANCE_NAME:-default}
    image: "geerlingguy/docker-${MOLECULE_DISTRO:-ubuntu2204}-ansible:latest"

provisioner:
  env:
    MY_VAR: ${MY_VAR}           # Simple substitution
    WITH_DEFAULT: ${VAR:-default}  # Default if unset or empty
    LITERAL: $$NOT_A_VAR       # Escaped dollar sign
```

**Important**: Avoid the `MOLECULE_` prefix for custom variables - it's reserved.

### Complete Example

```yaml
---
dependency:
  name: galaxy
  options:
    requirements-file: requirements.yml

driver:
  name: docker

platforms:
  - name: ubuntu-22
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true
    privileged: true
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw
    cgroupns_mode: host
    command: ""
    groups:
      - debian

  - name: rocky-9
    image: geerlingguy/docker-rockylinux9-ansible:latest
    pre_build_image: true
    privileged: true
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw
    cgroupns_mode: host
    command: ""
    groups:
      - redhat

provisioner:
  name: ansible
  log: true
  config_options:
    defaults:
      callbacks_enabled: profile_tasks
      fact_caching: jsonfile
      fact_caching_connection: /tmp/facts_cache
    ssh_connection:
      pipelining: true
  inventory:
    group_vars:
      all:
        ansible_user: root
  playbooks:
    converge: converge.yml
    verify: verify.yml
    prepare: prepare.yml

verifier:
  name: ansible

scenario:
  test_sequence:
    - dependency
    - cleanup
    - destroy
    - syntax
    - create
    - prepare
    - converge
    - idempotence
    - verify
    - cleanup
    - destroy
```

### Key Sections

#### dependency

Installs role and collection dependencies:

```yaml
dependency:
  name: galaxy
  options:
    requirements-file: requirements.yml
    # Force reinstall
    force: true
```

#### driver

Specifies the infrastructure driver. Molecule comes with three drivers pre-installed:

```yaml
# Docker (default, most common)
driver:
  name: docker

# Podman (rootless, no daemon required)
driver:
  name: podman

# Delegated (custom integration)
driver:
  name: delegated
```

**Podman vs Docker**:

- **Podman**: Lightweight, rootless mode for increased security, no running daemon
- **Docker**: More widely supported, better tooling ecosystem

**Flexible Backend with molecule-containers**:

For scenarios requiring backend flexibility, install `molecule-containers`:

```bash
pip install molecule-containers
```

Configure via environment variable:

```bash
export MOLECULE_CONTAINERS_BACKEND=podman,docker  # Prefer podman, fallback to docker
```

#### platforms

Defines test instances:

```yaml
platforms:
  - name: instance-name
    image: docker/image:tag
    pre_build_image: true      # Use image as-is, don't build
    privileged: true           # Required for systemd
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw
    cgroupns_mode: host        # For cgroups v2
    command: ""                # Override default command
    groups:                    # Ansible inventory groups
      - webservers
    docker_networks:           # Custom networks
      - name: molecule_net
```

#### provisioner

Configures Ansible execution:

```yaml
provisioner:
  name: ansible
  log: true
  config_options:
    defaults:
      callbacks_enabled: profile_tasks
      verbosity: 1
  env:
    ANSIBLE_DIFF_ALWAYS: "true"
  inventory:
    host_vars:
      instance-name:
        custom_var: value
    group_vars:
      all:
        common_var: value
```

#### scenario

Controls test execution sequence and scenario behavior:

```yaml
scenario:
  name: default
  test_sequence:
    - dependency    # Install dependencies
    - cleanup       # Initial cleanup
    - destroy       # Destroy existing instances
    - syntax        # Syntax check
    - create        # Create instances
    - prepare       # Run prepare playbook
    - converge      # Apply the role
    - idempotence   # Run converge again, fail on changes
    - verify        # Run verification
    - cleanup       # Final cleanup
    - destroy       # Destroy instances
```

**CRITICAL**: The `idempotence` step is mandatory unless explicitly documented.

**Task Filtering Tags**:

Skip tasks during specific Molecule phases using tags:

| Tag | Effect |
|-----|--------|
| `molecule-notest` | Skip task in ALL Molecule phases |
| `notest` | Alias for `molecule-notest` |
| `molecule-idempotence-notest` | Skip ONLY during idempotence check |

```yaml
# In your role task
- name: Seed database (not idempotent)
  ansible.builtin.command: /usr/bin/seed-db
  tags:
    - molecule-idempotence-notest  # Only skip during idempotence

- name: Task not suitable for containers
  ansible.builtin.reboot:
  tags:
    - molecule-notest  # Skip in all Molecule runs
```

**Shared State Between Scenarios**:

Enable resource sharing between scenarios to reduce execution time:

```yaml
# In molecule.yml
shared_state: true  # Access resources created by default scenario
```

When enabled, scenarios can access infrastructure created by the default scenario,
eliminating per-scenario setup/teardown overhead.

**Custom Sequences**:

Define separate sequences for different operations:

```yaml
scenario:
  create_sequence:
    - dependency
    - create
    - prepare
  converge_sequence:
    - converge
  destroy_sequence:
    - destroy
  test_sequence:
    - dependency
    - cleanup
    - destroy
    - syntax
    - create
    - prepare
    - converge
    - idempotence
    - verify
    - cleanup
    - destroy
```

#### Additional Settings

**Prerun Configuration**:

Molecule automatically installs dependencies via prerun. Disable if needed:

```yaml
prerun: false
```

**Role Name Check**:

By default, Molecule validates role names follow `namespace.role` standard.
Relax validation for non-conforming roles:

```yaml
role_name_check: 1  # Enable relaxed validation
```

**Note**: Following the namespace and role naming standard is strongly recommended.

---

## Converge Playbooks

The converge playbook applies the role under test.

### Standard Structure

```yaml
---
- name: Converge
  hosts: all
  become: true
  gather_facts: true

  vars:
    # Test-specific variable overrides
    nginx_port: 8080
    nginx_worker_processes: 2

  pre_tasks:
    - name: Update apt cache
      ansible.builtin.apt:
        update_cache: true
        cache_valid_time: 3600
      when: ansible_facts['os_family'] == 'Debian'

  roles:
    - role: "{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') | basename }}"
```

### Best Practices

1. **Use FQCN for all modules** - even in test playbooks

   ```yaml
   # Good
   - name: Update apt cache
     ansible.builtin.apt:
       update_cache: true

   # Bad
   - name: Update apt cache
     apt:
       update_cache: true
   ```

2. **Set become at play level** if role requires it

3. **Use environment variable for role name**

   ```yaml
   roles:
     - role: "{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') | basename }}"
   ```

4. **Keep converge minimal** - only what's needed to apply the role

5. **Use vars for test-specific overrides** - don't modify role defaults

### Role Inclusion Methods

```yaml
# Method 1: Direct role inclusion (preferred)
roles:
  - role: my_role
    vars:
      my_role_var: value

# Method 2: Include role task
tasks:
  - name: Include role under test
    ansible.builtin.include_role:
      name: my_role
    vars:
      my_role_var: value

# Method 3: With collection namespace
roles:
  - role: my_namespace.my_collection.my_role
```

---

## Verify Playbooks

The verify playbook validates that the role produced expected outcomes.

### Verifier Options

Molecule supports multiple verifiers:

| Verifier | Language | Best For |
|----------|----------|----------|
| **ansible** (default) | YAML | Simple tests (2-5 tasks), no extra language required |
| **testinfra** | Python | Complex test suites, parametrized tests, richer assertions |
| **goss** | YAML | Fast validation, simple syntax |

**Ansible** became the default verifier in Molecule v3 to provide a unified testing
experience without requiring Python knowledge for Testinfra.

**When to use Testinfra**:

- More than 5 verification tasks
- Need loops or complex conditional logic
- Evolving test suite with frequent additions
- Need parametrized tests across multiple values

**When to stick with Ansible**:

- Simple tests that fit in 2-5 tasks
- Team prefers YAML over Python
- Quick validation of basic outcomes

### Critical Requirements

**Every verify.yml MUST have assertions.** A verify playbook without assertions
is useless - it provides no validation.

### Standard Structure

```yaml
---
- name: Verify
  hosts: all
  become: true
  gather_facts: true

  vars_files:
    - ../../defaults/main.yml
    - ../../vars/main.yml

  tasks:
    # 1. Check package is installed
    - name: Check nginx is installed
      ansible.builtin.package:
        name: nginx
        state: present
      check_mode: true
      register: nginx_installed
      failed_when: nginx_installed.changed

    # 2. Check service is running
    - name: Gather service facts
      ansible.builtin.service_facts:

    - name: Assert nginx service is running
      ansible.builtin.assert:
        that:
          - "'nginx.service' in ansible_facts.services"
          - "ansible_facts.services['nginx.service'].state == 'running'"
        fail_msg: "nginx service is not running"
        success_msg: "nginx service is running as expected"

    # 3. Check file exists and has correct content
    - name: Read nginx config
      ansible.builtin.slurp:
        src: /etc/nginx/nginx.conf
      register: nginx_config

    - name: Assert nginx config contains expected content
      ansible.builtin.assert:
        that:
          - "'worker_processes' in nginx_config.content | b64decode"
        fail_msg: "nginx.conf missing worker_processes directive"

    # 4. Check port is listening
    - name: Check nginx is listening on port 80
      ansible.builtin.wait_for:
        port: 80
        timeout: 10
      register: port_check
      failed_when: port_check.failed

    # 5. Check HTTP response
    - name: Verify nginx responds
      ansible.builtin.uri:
        url: "http://localhost:80"
        status_code: 200
      register: http_response
      retries: 3
      delay: 5
      until: http_response.status == 200
```

### Assertion Patterns

#### Check Package Installation

```yaml
- name: Check package is installed
  ansible.builtin.package:
    name: "{{ package_name }}"
    state: present
  check_mode: true
  register: pkg_check
  failed_when: pkg_check.changed
```

#### Check Service Status

```yaml
- name: Gather service facts
  ansible.builtin.service_facts:

- name: Assert service is running and enabled
  ansible.builtin.assert:
    that:
      - "ansible_facts.services['{{ service_name }}.service'].state == 'running'"
      - "ansible_facts.services['{{ service_name }}.service'].status == 'enabled'"
    fail_msg: "{{ service_name }} is not running or not enabled"
```

#### Check File Existence and Permissions

```yaml
- name: Stat configuration file
  ansible.builtin.stat:
    path: /etc/app/config.yml
  register: config_stat

- name: Assert config file exists with correct permissions
  ansible.builtin.assert:
    that:
      - config_stat.stat.exists
      - config_stat.stat.mode == '0644'
      - config_stat.stat.pw_name == 'app'
    fail_msg: "Config file missing or has wrong permissions"
```

#### Check File Content

```yaml
- name: Read config file
  ansible.builtin.slurp:
    src: /etc/app/config.yml
  register: config_content

- name: Assert config contains expected values
  ansible.builtin.assert:
    that:
      - "'port: 8080' in config_content.content | b64decode"
      - "'debug: false' in config_content.content | b64decode"
    fail_msg: "Config file missing expected content"
```

#### Check Port Listening

```yaml
- name: Check application is listening
  ansible.builtin.wait_for:
    port: 8080
    host: localhost
    timeout: 30
  register: port_wait

- name: Assert port is open
  ansible.builtin.assert:
    that: not port_wait.failed
    fail_msg: "Application is not listening on port 8080"
```

#### Check HTTP Endpoint

```yaml
- name: Query health endpoint
  ansible.builtin.uri:
    url: http://localhost:8080/health
    return_content: true
  register: health_check

- name: Assert health check passes
  ansible.builtin.assert:
    that:
      - health_check.status == 200
      - "'healthy' in health_check.content"
    fail_msg: "Health check failed: {{ health_check.content }}"
```

### Weak vs Strong Assertions

```yaml
# WEAK - only checks file exists (not useful)
- name: Check config exists
  ansible.builtin.stat:
    path: /etc/app/config
  register: config_stat
# Missing assertion!

# STRONG - validates existence AND content
- name: Check config exists
  ansible.builtin.stat:
    path: /etc/app/config
  register: config_stat

- name: Assert config exists
  ansible.builtin.assert:
    that: config_stat.stat.exists
    fail_msg: "Config file does not exist"

- name: Read config
  ansible.builtin.slurp:
    src: /etc/app/config
  register: config_content

- name: Assert config has required settings
  ansible.builtin.assert:
    that:
      - "'database_host' in config_content.content | b64decode"
      - "'database_port' in config_content.content | b64decode"
    fail_msg: "Config missing required database settings"
```

---

## Multi-Platform Testing

Testing across multiple platforms catches platform-specific bugs.

### Platform Selection

Test all platforms your role supports:

```yaml
platforms:
  # Debian-family
  - name: ubuntu-20
    image: geerlingguy/docker-ubuntu2004-ansible:latest
    pre_build_image: true
    groups: [debian]

  - name: ubuntu-22
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true
    groups: [debian]

  - name: debian-11
    image: geerlingguy/docker-debian11-ansible:latest
    pre_build_image: true
    groups: [debian]

  # RedHat-family
  - name: rocky-8
    image: geerlingguy/docker-rockylinux8-ansible:latest
    pre_build_image: true
    groups: [redhat]

  - name: rocky-9
    image: geerlingguy/docker-rockylinux9-ansible:latest
    pre_build_image: true
    groups: [redhat]

  - name: fedora-39
    image: geerlingguy/docker-fedora39-ansible:latest
    pre_build_image: true
    groups: [redhat]
```

### Platform Groups

Use groups for platform-specific tests:

```yaml
# verify.yml
- name: Verify on Debian
  hosts: debian
  tasks:
    - name: Check apt package
      ansible.builtin.package:
        name: nginx
        state: present
      check_mode: true

- name: Verify on RedHat
  hosts: redhat
  tasks:
    - name: Check yum package
      ansible.builtin.package:
        name: nginx
        state: present
      check_mode: true
```

### Systemd Container Requirements

For containers running systemd:

```yaml
platforms:
  - name: ubuntu-systemd
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true
    privileged: true
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw
    cgroupns_mode: host
    command: ""
```

### Single Platform Anti-Pattern

**Problem**: Only testing on one platform when role supports multiple

```yaml
# BAD - Role supports Debian and RedHat but only tests Ubuntu
platforms:
  - name: ubuntu
    image: geerlingguy/docker-ubuntu2204-ansible:latest
```

**Solution**: Test all supported platforms

```yaml
# GOOD - Tests both OS families
platforms:
  - name: ubuntu-22
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    groups: [debian]

  - name: rocky-9
    image: geerlingguy/docker-rockylinux9-ansible:latest
    groups: [redhat]
```

---

## Idempotence Testing

Idempotence ensures a role can run multiple times safely.

### Why Idempotence Matters

1. **Repeated runs must not break things** - Production runs Ansible regularly
2. **Changes should only happen once** - Second run should report no changes
3. **Catches state bugs** - Reveals tasks that always report "changed"

### Enabling Idempotence

Include `idempotence` in test_sequence:

```yaml
scenario:
  test_sequence:
    - converge
    - idempotence  # MANDATORY
    - verify
```

### How It Works

1. Molecule runs converge
2. Molecule runs converge again
3. If any task reports "changed", the idempotence step fails

### Handling Non-Idempotent Tasks

Some tasks legitimately change every run. Handle with `changed_when`:

```yaml
# Non-idempotent by nature - use changed_when
- name: Get current timestamp
  ansible.builtin.command: date
  register: current_date
  changed_when: false  # Never report changed

# Conditional idempotence
- name: Run migration
  ansible.builtin.command: ./migrate.sh
  register: migration
  changed_when: "'Applied' in migration.stdout"
```

### Common Idempotence Failures

1. **command/shell without changed_when**

   ```yaml
   # BAD - always reports changed
   - name: Get status
     ansible.builtin.command: systemctl status nginx

   # GOOD - properly marked
   - name: Get status
     ansible.builtin.command: systemctl status nginx
     register: status
     changed_when: false
   ```

2. **Template with dynamic content**

   ```yaml
   # BAD - timestamp changes every run
   # Generated: {{ ansible_date_time.iso8601 }}

   # GOOD - use static marker
   # Generated by Ansible
   ```

3. **Missing creates/removes**

   ```yaml
   # BAD - runs every time
   - name: Initialize database
     ansible.builtin.command: ./init_db.sh

   # GOOD - only runs if marker missing
   - name: Initialize database
     ansible.builtin.command: ./init_db.sh
     args:
       creates: /var/lib/db/.initialized
   ```

---

## Prepare and Cleanup

### Prepare Playbook

Runs before converge to set up prerequisites:

```yaml
---
# prepare.yml
- name: Prepare
  hosts: all
  become: true
  gather_facts: true

  tasks:
    - name: Install required dependencies
      ansible.builtin.package:
        name:
          - python3
          - python3-pip
        state: present

    - name: Create required directories
      ansible.builtin.file:
        path: /opt/app
        state: directory
        mode: "0755"

    - name: Pre-populate configuration
      ansible.builtin.template:
        src: test-config.yml.j2
        dest: /etc/app/test-config.yml
```

### Cleanup Playbook

Runs after verify to clean up resources:

```yaml
---
# cleanup.yml
- name: Cleanup
  hosts: all
  become: true
  gather_facts: false

  tasks:
    - name: Remove test data
      ansible.builtin.file:
        path: /tmp/test-data
        state: absent

    - name: Stop test services
      ansible.builtin.service:
        name: test-service
        state: stopped
      ignore_errors: true
```

### When to Use Prepare/Cleanup

**Use prepare when:**

- Role requires pre-existing resources
- Testing upgrade scenarios
- Setting up test data

**Use cleanup when:**

- Tests create persistent resources
- Need to reset state between scenarios
- Running in shared environments

---

## Side Effects and Advanced Patterns

### Side Effect Playbook

The `side_effect.yml` playbook executes actions that produce side effects on instances.
It's designed for testing HA failover scenarios, service restarts, or configuration changes.

```yaml
---
# side_effect.yml
- name: Side Effect - Simulate failover
  hosts: all
  become: true
  tasks:
    - name: Stop primary service
      ansible.builtin.service:
        name: myapp
        state: stopped

    - name: Wait for failover
      ansible.builtin.pause:
        seconds: 10
```

### Advanced Multi-Step Testing

Molecule supports complex stateful testing with multiple side effects and verifications.
Actions can take optional arguments to specify different playbooks/tests:

```yaml
# molecule.yml
scenario:
  test_sequence:
    - converge
    - side_effect reboot.yaml        # Run specific side effect
    - verify after_reboot/           # Verify with tests in directory
    - side_effect alter_configs.yaml # Another side effect
    - converge                       # Re-converge after changes
    - verify test2.py test3.py       # Multiple test files
    - side_effect                    # Default side_effect.yml
    - verify                         # Default verify.yml
```

This pattern enables testing:

- **System reboots**: Verify state persists across restarts
- **Configuration drift**: Ensure role corrects manual changes
- **Upgrade scenarios**: Test transitions between versions
- **Failure recovery**: Validate HA and failover behavior

### Multiple Converge Steps

You can run converge multiple times with different configurations:

```yaml
scenario:
  test_sequence:
    - converge                    # Initial setup
    - converge upgrade.yml        # Upgrade scenario
    - idempotence
    - verify
```

---

## Development Workflow

### Iterative Development (Recommended)

For efficient role development, create instances once and iterate:

```bash
# 1. Create instance (one time)
molecule create

# 2. Development loop
molecule converge      # Apply role
# ... edit role tasks ...
molecule converge      # Re-apply (fast - no create/destroy)
molecule verify        # Check results
# ... repeat as needed ...

# 3. Full validation before commit
molecule test          # Complete test cycle

# 4. Cleanup when done
molecule destroy
```

This approach eliminates environment setup friction and provides rapid feedback.

### Test-Driven Development (TDD)

Write tests before implementation:

1. **Write verify.yml first** - Define expected outcomes
2. **Run `molecule converge`** - See tests fail (red)
3. **Implement role tasks** - Make tests pass (green)
4. **Run `molecule test`** - Validate everything including idempotence
5. **Refactor** - Improve code knowing tests catch regressions

### Quick Reference Commands

| Command | Purpose |
|---------|---------|
| `molecule create` | Create instances only |
| `molecule converge` | Apply role (keep instances) |
| `molecule idempotence` | Check idempotence only |
| `molecule verify` | Run verification only |
| `molecule test` | Full test cycle |
| `molecule destroy` | Remove instances |
| `molecule login` | SSH into instance |
| `molecule login -h instance-name` | SSH into specific instance |
| `molecule test --all` | Run all scenarios |
| `molecule test -s scenario-name` | Run specific scenario |
| `molecule list` | Show instance status |

### Debugging Failed Tests

```bash
# Keep instances after failure for debugging
molecule test --destroy=never

# Login to inspect state
molecule login

# Check Ansible output
molecule --debug test

# Run with verbose Ansible output
molecule converge -- -vvv
```

---

## CI/CD Integration

### Best Practices for CI/CD

1. **Use `fail-fast: false`** - When testing across multiple distributions, an error
   on one might not occur on another. Let all matrix jobs complete.

2. **Matrix test across distributions** - Use environment variables to test multiple
   OS versions in parallel.

3. **Cache dependencies** - Speed up builds by caching pip packages.

4. **Use colored output** - Set `PY_COLORS=1` and `ANSIBLE_FORCE_COLOR=1` for
   readable logs.

5. **Least privilege** - Grant only necessary permissions to workflow tokens.

### GitHub Actions Example

```yaml
name: Molecule Test

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  molecule:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false  # IMPORTANT: Let all distro tests complete
      matrix:
        distro:
          - ubuntu2204
          - ubuntu2404
          - debian12
          - rockylinux9
        scenario:
          - default

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Python
        uses: actions/setup-python@v5
        with:
          python-version: '3.12'
          cache: 'pip'

      - name: Install dependencies
        run: |
          pip install molecule molecule-plugins[docker] ansible-lint yamllint

      - name: Run Molecule
        run: molecule test -s ${{ matrix.scenario }}
        env:
          PY_COLORS: '1'
          ANSIBLE_FORCE_COLOR: '1'
          MOLECULE_DISTRO: ${{ matrix.distro }}
```

**Using MOLECULE_DISTRO in molecule.yml**:

```yaml
platforms:
  - name: instance
    image: "geerlingguy/docker-${MOLECULE_DISTRO:-ubuntu2204}-ansible:latest"
    pre_build_image: true
    privileged: true
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw
    cgroupns_mode: host
```

### Multi-Scenario Testing

```yaml
# Test multiple scenarios in parallel
jobs:
  molecule:
    strategy:
      fail-fast: false
      matrix:
        scenario:
          - default
          - security
          - upgrade

    steps:
      - name: Run Molecule
        run: molecule test -s ${{ matrix.scenario }}
```

### GitLab CI Example

```yaml
molecule:
  image: python:3.12
  services:
    - docker:dind
  variables:
    DOCKER_HOST: tcp://docker:2375
    PY_COLORS: '1'
    ANSIBLE_FORCE_COLOR: '1'
  before_script:
    - pip install molecule molecule-plugins[docker] ansible-lint
  script:
    - molecule test -s $MOLECULE_SCENARIO
  parallel:
    matrix:
      - MOLECULE_SCENARIO: [default, security]
        MOLECULE_DISTRO: [ubuntu2204, rockylinux9]
```

### Parallel Execution

Molecule supports parallel execution of scenarios:

```bash
# Run all scenarios in parallel
molecule test --parallel --all
```

**Note**: Ensure scenarios don't conflict when running in parallel (unique container
names, ports, etc.).

---

## Common Anti-Patterns

### 1. Missing Verify Playbook

**Problem**: No verify.yml means no validation

```yaml
# BAD - scenario with no verification
molecule/default/
├── molecule.yml
└── converge.yml
# Missing verify.yml!
```

**Solution**: Always include verify.yml with assertions

### 2. Empty Assertions

**Problem**: verify.yml exists but has no assertions

```yaml
# BAD - no actual assertions
- name: Verify
  hosts: all
  tasks:
    - name: Check file
      ansible.builtin.stat:
        path: /etc/app/config
      register: config_stat
      # Missing assert!
```

**Solution**: Add meaningful assertions

```yaml
# GOOD
- name: Assert config exists
  ansible.builtin.assert:
    that: config_stat.stat.exists
    fail_msg: "Config file does not exist"
```

### 3. Missing Idempotence

**Problem**: No idempotence step in test_sequence

```yaml
# BAD
scenario:
  test_sequence:
    - converge
    - verify
# Missing idempotence!
```

**Solution**: Always include idempotence

```yaml
# GOOD
scenario:
  test_sequence:
    - converge
    - idempotence
    - verify
```

### 4. Single Platform on Multi-Platform Role

**Problem**: Role supports multiple OS but tests only one

```yaml
# BAD - role claims to support Debian/RedHat
# but only tests Ubuntu
platforms:
  - name: ubuntu
    image: geerlingguy/docker-ubuntu2204-ansible:latest
```

**Solution**: Test all supported platforms

### 5. Non-FQCN in Test Playbooks

**Problem**: Using short module names

```yaml
# BAD
- name: Check package
  stat:
    path: /usr/bin/nginx
```

**Solution**: Use FQCN

```yaml
# GOOD
- name: Check package
  ansible.builtin.stat:
    path: /usr/bin/nginx
```

### 6. Missing gather_facts in Verify

**Problem**: Using ansible_facts without gathering them

```yaml
# BAD
- name: Verify
  hosts: all
  gather_facts: false  # or omitted
  tasks:
    - name: Check service
      ansible.builtin.assert:
        that: ansible_facts.services['nginx'].state == 'running'
        # Will fail - no facts gathered!
```

**Solution**: Enable gather_facts

```yaml
# GOOD
- name: Verify
  hosts: all
  gather_facts: true
  tasks:
    - name: Gather service facts
      ansible.builtin.service_facts:

    - name: Check service
      ansible.builtin.assert:
        that: ansible_facts.services['nginx.service'].state == 'running'
```

### 7. Hardcoded Values in Tests

**Problem**: Tests use hardcoded values that don't match role defaults

```yaml
# BAD - port hardcoded, doesn't match role
- name: Check port
  ansible.builtin.wait_for:
    port: 80  # Hardcoded!
```

**Solution**: Use vars_files or variables

```yaml
# GOOD
- name: Verify
  hosts: all
  vars_files:
    - ../../defaults/main.yml
  tasks:
    - name: Check port
      ansible.builtin.wait_for:
        port: "{{ nginx_port }}"  # From role defaults
```

### 8. Testing Implementation Details

**Problem**: Tests verify internal implementation rather than observable outcomes

```yaml
# BAD - testing implementation detail
- name: Check temp file exists
  ansible.builtin.stat:
    path: /tmp/role-internal-marker
  register: marker

- name: Assert marker exists
  ansible.builtin.assert:
    that: marker.stat.exists
```

**Solution**: Test observable behavior and outcomes

```yaml
# GOOD - testing actual outcome
- name: Verify service responds correctly
  ansible.builtin.uri:
    url: http://localhost:8080/health
    status_code: 200
```

### 9. Excessive Setup in Converge

**Problem**: Converge playbook does too much beyond applying the role

```yaml
# BAD - converge does setup work
- name: Converge
  hosts: all
  tasks:
    - name: Install prerequisites
      ansible.builtin.package:
        name: [python3, curl, wget]
        state: present
    - name: Create directories
      ansible.builtin.file:
        path: /opt/app
        state: directory
    # ... lots of setup ...
    - name: Include role
      ansible.builtin.include_role:
        name: my_role
```

**Solution**: Move setup to prepare.yml

```yaml
# GOOD - converge is minimal
- name: Converge
  hosts: all
  roles:
    - role: my_role
```

### 10. Testing-Only Dependencies Not Isolated

**Problem**: Role dependencies installed unconditionally

**Solution**: Use environment variable to conditionally include test dependencies

```yaml
# In meta/main.yml
dependencies:
  - role: test_helper_role
    when: lookup('env', 'MOLECULE_FILE') | length > 0
```

---

## Quality Checklist

### molecule.yml

- [ ] Driver configured (docker/podman)
- [ ] Multiple platforms if role supports multiple OS
- [ ] Platform groups defined for OS-specific tests
- [ ] Pre-built images used for faster CI (`pre_build_image: true`)
- [ ] Privileged mode and cgroup mounts for systemd containers
- [ ] Provisioner has sensible defaults
- [ ] test_sequence includes `idempotence`
- [ ] Dependencies specified in requirements.yml if needed
- [ ] Environment variable substitution for flexible testing

### converge.yml

- [ ] All modules use FQCN
- [ ] Role included correctly using basename lookup or explicit name
- [ ] Test variables set appropriately (not modifying defaults)
- [ ] `become: true` set if role requires privilege
- [ ] `gather_facts: true` enabled if role needs facts
- [ ] Minimal - only what's needed to apply the role

### verify.yml

- [ ] **Has assertions** - not just checks without assert
- [ ] All modules use FQCN
- [ ] `gather_facts: true` if using ansible_facts
- [ ] `service_facts` gathered before checking services
- [ ] `check_mode: true` used for non-mutating package/service checks
- [ ] Meaningful `fail_msg` on all assertions
- [ ] Meaningful `success_msg` for clarity (optional)
- [ ] Tests actual outcomes, not just existence
- [ ] `vars_files` loads role defaults if referencing role variables
- [ ] Tests observable behavior, not implementation details

### prepare.yml (if used)

- [ ] Only contains prerequisite setup
- [ ] Doesn't duplicate role functionality
- [ ] Minimal and fast

### Idempotence

- [ ] `changed_when: false` on commands that don't change state
- [ ] `creates`/`removes` arguments on command tasks where applicable
- [ ] No dynamic content in templates (timestamps, random values)
- [ ] `molecule-idempotence-notest` tag on legitimately non-idempotent tasks

### General

- [ ] No syntax errors (`molecule syntax`)
- [ ] Passes ansible-lint
- [ ] Passes yamllint
- [ ] Tests complete in reasonable time (< 5 minutes for CI)
- [ ] Clear, actionable failure messages
- [ ] CI uses `fail-fast: false` for matrix jobs
- [ ] All supported platforms tested

---

## Troubleshooting

### Common Issues

**Idempotence fails but role works correctly**:

1. Run `molecule converge` twice manually
2. Check which tasks report `changed`
3. Add `changed_when: false` or use creates/removes

**Service facts empty**:

```yaml
# Must explicitly gather service facts
- name: Gather service facts
  ansible.builtin.service_facts:

- name: Check service
  ansible.builtin.assert:
    that: ansible_facts.services['nginx.service'].state == 'running'
```

**Container fails to start with systemd**:

```yaml
platforms:
  - name: instance
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true
    privileged: true              # Required
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw  # Required
    cgroupns_mode: host           # Required for cgroups v2
    command: ""                   # Don't override entrypoint
```

**Role library/modules not available in verify.yml**:

```yaml
# Include role with empty task to make library available
- name: Verify
  hosts: all
  tasks:
    - name: Include role for library access
      ansible.builtin.include_role:
        name: my_role
        tasks_from: init.yml  # Empty task file
```

---

## References

### Official Documentation

- [Molecule Documentation](https://docs.ansible.com/projects/molecule/)
- [Molecule Testing Philosophy](https://docs.ansible.com/projects/molecule/philosophy/)
- [Molecule Configuration Reference](https://docs.ansible.com/projects/molecule/configuration/)
- [Molecule FAQ](https://ansible.readthedocs.io/projects/molecule/faq/)
- [Molecule CI/CD Integration](https://docs.ansible.com/projects/molecule/ci/)

### Community Resources

- [Jeff Geerling's Testing Guide](https://www.jeffgeerling.com/blog/2018/testing-your-ansible-roles-molecule)
- [Test-Driven Ansible with Molecule](https://hashbangwallop.com/tdd-ansible.html)
- [Developing with Molecule and Podman - Red Hat](https://www.ansible.com/blog/developing-and-testing-ansible-roles-with-molecule-and-podman-part-1/)
- [Ansible Testing with Molecule Tutorial](https://yrkan.com/blog/ansible-testing-with-molecule/)
- [Testing Ansible Automation with Molecule](https://www.endpointdev.com/blog/2025/03/testing-ansible-with-molecule/)

### Related Tools

- [Ansible Lint](https://docs.ansible.com/projects/lint/)
- [yamllint](https://yamllint.readthedocs.io/)
- [Testinfra Documentation](https://testinfra.readthedocs.io/)
- [Geerlingguy Docker Images](https://github.com/geerlingguy/docker-ubuntu2204-ansible)

---

_Last updated: 2026-02-06_
