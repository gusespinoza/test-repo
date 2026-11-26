import { Routes } from '@angular/router';

import { AdminGuard } from './core/auth/admin.guard';

export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'events' },
  {
    path: 'events',
    loadComponent: () =>
      import('./features/events/events-list.component').then((module) => module.EventsListComponent)
  },
  {
    path: 'events/:id',
    loadComponent: () =>
      import('./features/events/event-detail.component').then((module) => module.EventDetailComponent)
  },
  {
    path: 'bookings/new',
    loadComponent: () =>
      import('./features/bookings/booking-selection.component').then(
        (module) => module.BookingSelectionComponent
      )
  },
  {
    path: 'bookings/:id',
    loadComponent: () =>
      import('./features/bookings/booking-checkout.component').then(
        (module) => module.BookingCheckoutComponent
      )
  },
  {
    path: 'admin/events',
    canActivate: [AdminGuard],
    loadComponent: () =>
      import('./features/admin/admin-events.component').then((module) => module.AdminEventsComponent)
  },
  {
    path: 'admin/bookings',
    canActivate: [AdminGuard],
    loadComponent: () =>
      import('./features/admin/admin-bookings.component').then(
        (module) => module.AdminBookingsComponent
      )
  },
  { path: '**', redirectTo: 'events' }
];
