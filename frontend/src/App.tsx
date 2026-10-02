import { AppBar, Container, CssBaseline, ThemeProvider, Toolbar, Typography, createTheme } from '@mui/material';
import { BrowserRouter, Link, Route, Routes } from 'react-router';
import { NamespacePage } from './pages/NamespacePage';
import { OverviewPage } from './pages/OverviewPage';

const theme = createTheme({ colorSchemes: { dark: true } });

export function App() {
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <BrowserRouter>
        <AppBar position="static">
          <Toolbar>
            <Typography variant="h6" component={Link} to="/" sx={{ color: 'inherit', textDecoration: 'none' }}>
              Feature-Dashboard
            </Typography>
          </Toolbar>
        </AppBar>
        <Container maxWidth="xl" sx={{ py: 3 }}>
          <Routes>
            <Route path="/" element={<OverviewPage />} />
            <Route path="/projects/:project/namespaces/:namespace" element={<NamespacePage />} />
            <Route path="*" element={<OverviewPage />} />
          </Routes>
        </Container>
      </BrowserRouter>
    </ThemeProvider>
  );
}
