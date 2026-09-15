# salt/ — Estados de SaltStack (espello dos playbooks de Ansible)

Cando o motor activo é **Salt**, cada acción con nome lóxico aplícase con
`salt <host> state.apply <estado>`. O nome do `.sls` resólvese en
`config.yaml → salt.estados` (igual que `ansible.playbooks`).

| Nome lóxico        | Playbook Ansible          | Estado Salt (.sls)        | Estado |
|--------------------|---------------------------|---------------------------|--------|
| acender            | acenderAula.yaml          | *(non aplica — ver abaixo)* | ✅ feito en Go |
| durmir             | durmirAula.yaml           | durmirAula.sls            | ✅ feito |
| bloqueo            | bloqueoTotal.yaml         | bloqueoTotal.sls          | ✅ exemplo |
| liberar            | desbloquearAula.yaml      | desbloquearAula.sls       | ✅ feito |
| apagar             | apagar_aula.yaml          | apagar_aula.sls           | ✅ feito |
| aviso_ruido        | avisoRuido.yaml           | avisoRuido.sls            | ✅ feito |
| comprobar_bloqueo  | comprobar_bloqueo.yaml    | comprobar_bloqueo.sls     | ✅ feito (informativo) |

### Por que "acender" non ten .sls

Un equipo apagado non ten `salt-minion` en marcha, así que nunca pode recibir
un `state.apply` — non hai forma de "aplicarlle un estado" a unha máquina sen
enerxía. En Ansible isto resólvese con `delegate_to: localhost` (o propio
servidor envía o paquete Wake-on-LAN). En Salt, o equivalente non é un estado
de minion senón unha acción do propio master, así que `internal/motor/salt.go`
trata `"acender"` como caso especial: le a MAC do `hosts` e executa
`wakeonlan` directamente dende o servidor, sen pasar por `state.apply`.

## Como traducir un playbook a un estado

Os playbooks son maioritariamente bloques `shell:`. O equivalente máis directo
en Salt é un estado `cmd.run` co mesmo bash:

```yaml
# playbooks/exemplo.yaml (Ansible)
- shell: |
    echo ola

# salt/exemplo.sls (Salt)
o_meu_paso:
  cmd.run:
    - name: |
        echo ola
```

Notas:
- As variables `{{ usuario_alumno }}` de Ansible pasan a valores fixos ou a
  **pillar** de Salt (`{{ pillar['usuario'] }}`).
- Tarefas con `delegate_to: localhost` (ex. Wake-on-LAN en `acenderAula`)
  execútanse no **master**, non no minion: fanse cun runner/orquestración ou
  desde o propio Piztu, non nun `.sls` de minion.

## Requisitos no servidor
- `salt-master` en execución; `salt-minion` en cada equipo coa id == hostname.
- Chaves aceptadas: `salt-key -A`.
- Para recoller prácticas (`cp.push`): engadir `file_recv: True` no master.
