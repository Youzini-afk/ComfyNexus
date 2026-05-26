import { Routes, Route, Navigate } from 'react-router-dom';
import { IndexRedirect } from './index';
import { LoginPage } from './login';
import { SetupPage } from './setup';
import { AppLayout } from './_authed';
import { Workbench } from './_authed/workbench';
import { InstancesPage } from './_authed/instances';
import {
  FilesPage,
  ModelsPage,
  ImagesPage,
  DownloadsPage,
  SystemPage,
  SettingsPage,
} from './_authed/placeholder-pages';

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<IndexRedirect />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/setup" element={<SetupPage />} />
      <Route element={<AppLayout />}>
        <Route path="/workbench" element={<Workbench />} />
        <Route path="/files" element={<FilesPage />} />
        <Route path="/models" element={<ModelsPage />} />
        <Route path="/images" element={<ImagesPage />} />
        <Route path="/downloads" element={<DownloadsPage />} />
        <Route path="/system" element={<SystemPage />} />
        <Route path="/instances" element={<InstancesPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
