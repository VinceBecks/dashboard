import { AppBar, Box, Container, CssBaseline, ThemeProvider, Toolbar, Typography, createTheme } from '@mui/material';
import { BrowserRouter, Link, Route, Routes } from 'react-router';
import { PollingControl } from './components/PollingControl';
import { PollError } from './components/StatusViews';
import { PollingSettingsProvider, usePollingSettings } from './hooks/PollingSettings';
import { NamespacePage } from './pages/NamespacePage';
import { OverviewPage } from './pages/OverviewPage';

const theme = createTheme({ colorSchemes: { dark: true } });

export function App() {
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <BrowserRouter>
        <PollingSettingsProvider>
          <Layout />
        </PollingSettingsProvider>
      </BrowserRouter>
    </ThemeProvider>
  );
}

function Layout() {
  const { error } = usePollingSettings();
  return (
    <>
      <AppBar position="static">
        <Toolbar>
          <Typography variant="h6" component={Link} to="/" sx={{ color: 'inherit', textDecoration: 'none' }}>
            Feature-Dashboard
          </Typography>
          <Box sx={{ flexGrow: 1 }} />
          <PollingControl />
        </Toolbar>
      </AppBar>
      <Container maxWidth="xl" sx={{ py: 3 }}>
        {error && <PollError error={error} />}
        <Routes>
          <Route path="/" element={<OverviewPage />} />
          <Route path="/projects/:project/namespaces/:namespace" element={<NamespacePage />} />
          <Route path="*" element={<OverviewPage />} />
        </Routes>
      </Container>
    </>
  );
}
