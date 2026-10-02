import {
  Alert,
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  FormHelperText,
  InputLabel,
  ListItemText,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import { useState } from 'react';
import { ApiError, executeAction } from '../api/client';
import type { Action, ActionResult, FormField, FormValue, ObjectRef } from '../api/types';

interface Props {
  action: Action;
  object: ObjectRef;
  onClose: () => void;
  onResult: (result: ActionResult) => void;
}

/** Bestätigungs- und Formular-Dialog für eine Aktion. Die Felder kommen vollständig aus dem Backend. */
export function ActionDialog({ action, object, onClose, onResult }: Props) {
  const fields = action.formFields ?? [];
  const [values, setValues] = useState<Record<string, FormValue>>(() =>
    Object.fromEntries(fields.map((f) => [f.id, f.type === 'multi-select' ? [] : ''])),
  );
  const [touched, setTouched] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<ApiError | Error>();

  const missing = fields.filter((f) => f.isRequired && isEmpty(values[f.id])).map((f) => f.id);

  const submit = async () => {
    setTouched(true);
    if (missing.length > 0) {
      return;
    }
    setSubmitting(true);
    setError(undefined);
    try {
      onResult(await executeAction(action.id, object, toInputs(fields, values)));
    } catch (e) {
      setError(e as Error);
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onClose={submitting ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>{action.title}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <Typography variant="body2" color="text.secondary">
            Objekt: {object.kind} <strong>{object.name}</strong>
            {object.kind !== 'namespace' && <> im Namespace {object.namespace}</>}
          </Typography>
          {fields.map((f) => (
            <Field
              key={f.id}
              field={f}
              value={values[f.id]}
              showError={touched && missing.includes(f.id)}
              onChange={(v) => setValues((old) => ({ ...old, [f.id]: v }))}
            />
          ))}
          {error && (
            <Alert severity="error">
              {error.message}
              {error instanceof ApiError && error.problems.length > 0 && (
                <ul>
                  {error.problems.map((p) => (
                    <li key={p}>{p}</li>
                  ))}
                </ul>
              )}
            </Alert>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          Abbrechen
        </Button>
        <Button variant="contained" onClick={() => void submit()} disabled={submitting}>
          Ausführen
        </Button>
      </DialogActions>
    </Dialog>
  );
}

interface FieldProps {
  field: FormField;
  value: FormValue;
  showError: boolean;
  onChange: (value: FormValue) => void;
}

function Field({ field, value, showError, onChange }: FieldProps) {
  const helperText = showError ? 'Pflichtfeld' : field.description;
  const id = `action-field-${field.id}`;

  if (field.type === 'select' || field.type === 'multi-select') {
    const multiple = field.type === 'multi-select';
    const options = field.options ?? [];
    const labelOf = (v: string) => options.find((o) => o.value === v)?.label ?? v;
    return (
      <FormControl fullWidth required={field.isRequired} error={showError}>
        <InputLabel id={`${id}-label`}>{field.label}</InputLabel>
        <Select
          labelId={`${id}-label`}
          id={id}
          label={field.label}
          multiple={multiple}
          value={value}
          onChange={(e) => onChange(e.target.value as FormValue)}
          renderValue={multiple ? (selected) => (selected as string[]).map(labelOf).join(', ') : undefined}
        >
          {options.map((o) => (
            <MenuItem key={o.value} value={o.value}>
              {multiple && <Checkbox checked={(value as string[]).includes(o.value)} />}
              <ListItemText primary={o.label} />
            </MenuItem>
          ))}
        </Select>
        {helperText && <FormHelperText>{helperText}</FormHelperText>}
      </FormControl>
    );
  }

  return (
    <TextField
      id={id}
      label={field.label}
      type={field.type === 'number' ? 'number' : 'text'}
      required={field.isRequired}
      error={showError}
      helperText={helperText}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      fullWidth
    />
  );
}

function isEmpty(v: FormValue | undefined): boolean {
  return v === undefined || v === '' || (Array.isArray(v) && v.length === 0);
}

function toInputs(fields: FormField[], values: Record<string, FormValue>): Record<string, FormValue> {
  const inputs: Record<string, FormValue> = {};
  for (const f of fields) {
    const v = values[f.id];
    if (isEmpty(v)) {
      continue;
    }
    inputs[f.id] = f.type === 'number' ? Number(v) : v;
  }
  return inputs;
}
