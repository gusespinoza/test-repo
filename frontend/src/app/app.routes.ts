import { Routes } from '@angular/router';

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
  { path: '**', redirectTo: 'events' }
];
