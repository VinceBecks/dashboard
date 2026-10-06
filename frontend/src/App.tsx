import { AppBar, Box, Container, CssBaseline, ThemeProvider, Toolbar, Typography, createTheme } from '@mui/material';
import { ConnectionStatus } from './components/ConnectionStatus';
import { useDashboardEvents } from './hooks/useDashboardEvents';
import { DashboardPage } from './pages/DashboardPage';

const theme = createTheme({ colorSchemes: { dark: true } });

export function App() {
  const { data, connection } = useDashboardEvents();
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <AppBar position="static">
        <Toolbar>
          <Typography variant="h6">Feature-Dashboard</Typography>
          <Box sx={{ flexGrow: 1 }} />
          <ConnectionStatus state={connection} />
        </Toolbar>
      </AppBar>
      <Container maxWidth="xl" sx={{ py: 3 }}>
        <DashboardPage data={data} />
      </Container>
    </ThemeProvider>
  );
}
