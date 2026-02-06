# Molecule Testing Guide

A comprehensive guide to writing quality Molecule tests for Ansible roles and playbooks.
This document serves as the knowledge base for the ansible-molecule agent.

---

## Table of Contents

- [Overview](#overview)
- [Scenario Structure](#scenario-structure)
- [molecule.yml Configuration](#moleculeyml-configuration)
- [Converge Playbooks](#converge-playbooks)
- [Verify Playbooks](#verify-playbooks)
- [Multi-Platform Testing](#multi-platform-testing)
- [Idempotence Testing](#idempotence-testing)
- [Prepare and Cleanup](#prepare-and-cleanup)
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

The `molecule.yml` file configures the test scenario.

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

Specifies the infrastructure driver:

```yaml
# Docker (most common)
driver:
  name: docker

# Podman
driver:
  name: podman

# Vagrant
driver:
  name: vagrant
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

Controls test execution sequence:

```yaml
scenario:
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

## CI/CD Integration

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
      matrix:
        scenario:
          - default
          - security
      fail-fast: false

    steps:
      - uses: actions/checkout@v4

      - name: Set up Python
        uses: actions/setup-python@v5
        with:
          python-version: '3.11'

      - name: Install dependencies
        run: |
          pip install molecule molecule-docker ansible-lint

      - name: Run Molecule
        run: molecule test -s ${{ matrix.scenario }}
        env:
          PY_COLORS: '1'
          ANSIBLE_FORCE_COLOR: '1'
```

### GitLab CI Example

```yaml
molecule:
  image: python:3.11
  services:
    - docker:dind
  variables:
    DOCKER_HOST: tcp://docker:2375
  before_script:
    - pip install molecule molecule-docker ansible-lint
  script:
    - molecule test
  parallel:
    matrix:
      - MOLECULE_SCENARIO: [default, security]
```

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

---

## Quality Checklist

### molecule.yml

- [ ] Driver configured (docker/podman/vagrant)
- [ ] Multiple platforms if role supports multiple OS
- [ ] Platform groups defined for OS-specific tests
- [ ] Provisioner has sensible defaults
- [ ] test_sequence includes `idempotence`
- [ ] Dependencies specified if needed

### converge.yml

- [ ] All modules use FQCN
- [ ] Role included correctly
- [ ] Test variables set appropriately
- [ ] become set if role requires it
- [ ] gather_facts enabled if needed

### verify.yml

- [ ] **Has assertions** - not just checks
- [ ] All modules use FQCN
- [ ] gather_facts enabled if using facts
- [ ] check_mode used for non-mutating checks
- [ ] Meaningful fail_msg on assertions
- [ ] Tests actual outcomes, not just existence
- [ ] vars_files loads role defaults if needed

### General

- [ ] No syntax errors
- [ ] Passes ansible-lint
- [ ] Passes yamllint
- [ ] Tests complete in reasonable time
- [ ] Clear failure messages

---

## References

- [Molecule Documentation](https://molecule.readthedocs.io/)
- [Ansible Testing Strategies](https://docs.ansible.com/ansible/latest/reference_appendices/test_strategies.html)
- [Jeff Geerling's Testing Guide](https://www.jeffgeerling.com/blog/2018/testing-your-ansible-roles-molecule)
- [Ansible Lint](https://docs.ansible.com/projects/lint/)

---

_Last updated: 2026-02-06_
